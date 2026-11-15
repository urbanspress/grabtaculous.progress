package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"grabtaculous/internal/catalog"
	"grabtaculous/internal/container"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

// DeployOptions configures a curated 1-click marketplace stack deployment.
type DeployOptions struct {
	BlueprintID    string
	TargetDir      string
	StateDir       string
	CatalogDir     string
	AppName        string
	Domain         string
	EdgeProxy      string
	Workers        int
	GrabmanBinary  string
	SkipLockVerify bool
	ReadyURLs      []string
}

// DeployResult summarizes a completed or rolled-back blueprint deployment.
type DeployResult struct {
	SchemaVersion      string   `json:"schema_version"`
	BlueprintID        string   `json:"blueprint_id"`
	BlueprintVersion   string   `json:"blueprint_version"`
	BlueprintSHA256    string   `json:"blueprint_sha256"`
	AppName            string   `json:"app_name"`
	Workspace          string   `json:"workspace"`
	EdgeProxy          string   `json:"edge_proxy"`
	Domain             string   `json:"domain"`
	WorkerPorts        []int    `json:"worker_ports"`
	ComposeFile        string   `json:"compose_file"`
	EnvExampleFile     string   `json:"env_example_file"`
	EdgeFile           string   `json:"edge_file"`
	PreDeploySnapshot  string   `json:"pre_deploy_snapshot,omitempty"`
	DeploySnapshotID   string   `json:"deploy_snapshot_id"`
	FilesWritten       []string `json:"files_written"`
	LockfileVerified   bool     `json:"lockfile_verified"`
}

// DeployBlueprint validates a curated blueprint, enforces lockfile & container hardening,
// captures a Pre-Deploy Snapshot, renders --read-only Compose + Edge TLS manifests, and updates inventory.
func DeployBlueprint(ctx context.Context, opts DeployOptions) (*DeployResult, error) {
	loader := catalog.NewLoader(opts.CatalogDir)
	bp, err := loader.LoadBlueprint(opts.BlueprintID)
	if err != nil {
		return nil, err
	}

	targetWS, err := safepath.InitRoot(opts.TargetDir)
	if err != nil {
		return nil, err
	}

	appName := strings.TrimSpace(opts.AppName)
	if appName == "" {
		appName = bp.ID
	}
	edgeProxy := strings.ToLower(strings.TrimSpace(opts.EdgeProxy))
	if edgeProxy == "" {
		edgeProxy = bp.RecommendedEdgeProxy
	}
	if edgeProxy == "" {
		edgeProxy = "caddy"
	}
	domain := strings.TrimSpace(opts.Domain)
	if domain == "" {
		domain = "localhost"
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(targetWS, ".grabtaculous")
	}

	// Verify existing grabman.lock if present in workspace.
	lockVerified := false
	if !opts.SkipLockVerify {
		if gmAbs, _, gErr := safepath.ResolveFile(targetWS, "grabman.json", safepath.ResolveOptions{AllowMissing: true}); gErr == nil {
			if _, statErr := os.Stat(gmAbs); statErr == nil {
				grabmanBin := rollout.LocateGrabmanBinary(opts.GrabmanBinary)
				if grabmanBin == "" {
					return nil, safepath.NewError(safepath.ErrPreflightFailed, "grabman binary not found to verify workspace grabman.lock", "Install grabman v1.0.0 or pass --grabman-bin.")
				}
				cmd := exec.CommandContext(ctx, grabmanBin, "solve", "--dir", targetWS, "--verify", "--json")
				if out, runErr := cmd.CombinedOutput(); runErr != nil {
					return nil, safepath.WrapError(safepath.ErrPreflightFailed, fmt.Sprintf("grabman solve --verify failed before deploy: %s", strings.TrimSpace(string(out))), "Run 'grabman solve' in the workspace before deploying.", runErr)
				}
				lockVerified = true
			}
		}
	}

	store, err := NewInventoryStore(stateDir)
	if err != nil {
		return nil, err
	}
	workerCount := opts.Workers
	if workerCount <= 0 {
		workerCount = len(bp.Stack.Services)
		if workerCount < 1 {
			workerCount = 1
		}
	}
	ports, err := store.AllocatePorts(appName, workerCount)
	if err != nil {
		return nil, err
	}

	// Capture Pre-Deploy Snapshot if workspace already has deployable artifacts.
	var preSnapID string
	if hasExistingDeployState(targetWS) {
		preRec, sErr := snapshot.Create(ctx, snapshot.CreateOptions{
			StateDir:  stateDir,
			Workspace: targetWS,
			AppName:   appName,
			Trigger:   "pre-deploy",
		})
		if sErr != nil {
			return nil, sErr
		}
		preSnapID = preRec.DeployID
	}

	// Clone and specialize StackSpec with loopback ports & chosen edge proxy.
	stack := bp.Stack
	stack.StackName = appName
	stack.EdgeProxy = edgeProxy
	stack.Domain = domain
	services := make([]container.ServiceSpec, len(bp.Stack.Services))
	for i, svc := range bp.Stack.Services {
		svcCopy := svc
		if !svcCopy.IsDataStore && i < len(ports) && len(svcCopy.Ports) > 0 {
			// Replace default loopback port binding cleanly while keeping 127.0.0.1 host binding.
			containerPort := extractContainerPort(svcCopy.Ports[0])
			if containerPort != "" {
				svcCopy.Ports = []string{fmt.Sprintf("127.0.0.1:%d:%s", ports[i], containerPort)}
			}
		}
		services[i] = svcCopy
	}
	stack.Services = services

	composeBytes, err := container.RenderComposeYAML(stack)
	if err != nil {
		return nil, err
	}
	if err := container.AuditComposeYAML("deploy/docker-compose.yml", string(composeBytes), container.AuditOptions{RequireDigest: true}); err != nil {
		return nil, err
	}
	envExampleBytes := container.RenderEnvExample(stack)

	edgeArt, err := container.RenderEdgeConfig(container.EdgeRouteConfig{
		EdgeProxy:     edgeProxy,
		Domain:        domain,
		UpstreamHost:  "127.0.0.1",
		UpstreamPorts: ports,
		HealthPath:    "/_ready",
	})
	if err != nil {
		return nil, err
	}

	if err := safepath.WriteFileAtomic(targetWS, "deploy/docker-compose.yml", composeBytes, 0o644, safepath.ResolveOptions{}); err != nil {
		return nil, err
	}
	if err := safepath.WriteFileAtomic(targetWS, "deploy/.env.example", envExampleBytes, 0o644, safepath.ResolveOptions{AllowEnvExample: true}); err != nil {
		return nil, err
	}
	if err := safepath.WriteFileAtomic(targetWS, edgeArt.Filename, edgeArt.Content, 0o644, safepath.ResolveOptions{}); err != nil {
		return nil, err
	}

	postRec, err := snapshot.Create(ctx, snapshot.CreateOptions{
		StateDir:  stateDir,
		Workspace: targetWS,
		AppName:   appName,
		Trigger:   "deploy",
	})
	if err != nil {
		return nil, err
	}

	if len(opts.ReadyURLs) > 0 {
		for _, rURL := range opts.ReadyURLs {
			base := strings.TrimSuffix(rURL, "/_ready")
			if waitErr := rollout.WaitReady(ctx, base, rollout.DefaultReadyTimeout, 0); waitErr != nil {
				if preSnapID != "" {
					_, _ = snapshot.Rollback(ctx, snapshot.RollbackOptions{
						StateDir:  stateDir,
						Workspace: targetWS,
						DeployID:  preSnapID,
					})
				}
				return nil, waitErr
			}
		}
	}

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/_health", ports[0])
	readyURL := fmt.Sprintf("http://127.0.0.1:%d/_ready", ports[0])
	if _, err := store.UpsertApp(AppRecord{
		AppName:        appName,
		BlueprintID:    bp.ID,
		Workspace:      targetWS,
		EdgeProxy:      edgeProxy,
		Domain:         domain,
		WorkerPorts:    ports,
		ActiveDeployID: postRec.DeployID,
		ComposeFile:    "deploy/docker-compose.yml",
		EdgeFile:       edgeArt.Filename,
		HealthURL:      healthURL,
		ReadyURL:       readyURL,
		Status:         "deployed",
	}); err != nil {
		return nil, err
	}

	return &DeployResult{
		SchemaVersion:     "1.0.0",
		BlueprintID:       bp.ID,
		BlueprintVersion:  bp.Version,
		BlueprintSHA256:   bp.VerifiedManifestSHA256,
		AppName:           appName,
		Workspace:         targetWS,
		EdgeProxy:         edgeProxy,
		Domain:            domain,
		WorkerPorts:       ports,
		ComposeFile:       "deploy/docker-compose.yml",
		EnvExampleFile:    "deploy/.env.example",
		EdgeFile:          edgeArt.Filename,
		PreDeploySnapshot: preSnapID,
		DeploySnapshotID:  postRec.DeployID,
		FilesWritten:      []string{"deploy/docker-compose.yml", "deploy/.env.example", edgeArt.Filename},
		LockfileVerified:  lockVerified,
	}, nil
}

func extractContainerPort(mapping string) string {
	parts := strings.Split(strings.TrimSpace(mapping), ":")
	if len(parts) < 2 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if _, err := strconv.Atoi(last); err != nil {
		return ""
	}
	return last
}

func hasExistingDeployState(ws string) bool {
	for _, rel := range []string{"deploy/docker-compose.yml", "grabman.lock", "src/app.grab", "storage/site-dna.json"} {
		abs, _, err := safepath.ResolveFile(ws, rel, safepath.ResolveOptions{AllowMissing: true})
		if err == nil {
			if st, sErr := os.Stat(abs); sErr == nil && st.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}
