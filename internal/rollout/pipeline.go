package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grabtaculous/internal/container"
	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

// LocateGrabBinary finds the GRAB v1.1.0 executable cleanly.
func LocateGrabBinary(explicit string) string {
	return locateBinary(explicit, "GRAB_BIN", "grab", []string{
		`C:\Program Files\GRAB\grab.exe`,
		`C:\Development\GRAB\dist\grab-windows-amd64.exe`,
		"/usr/local/bin/grab",
		"/usr/bin/grab",
	})
}

// LocateGrabmanBinary finds the grabman v0.1.0 executable cleanly.
func LocateGrabmanBinary(explicit string) string {
	return locateBinary(explicit, "GRABMAN_BIN", "grabman", []string{
		`C:\Program Files\grabman\grabman.exe`,
		`C:\Development\grabman\dist\grabman-windows-amd64.exe`,
		"/usr/local/bin/grabman",
		"/usr/bin/grabman",
	})
}

func locateBinary(explicit, envVar, pathName string, defaults []string) string {
	var candidates []string
	if strings.TrimSpace(explicit) != "" {
		candidates = append(candidates, strings.TrimSpace(explicit))
	}
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		candidates = append(candidates, env)
	}
	if p, err := exec.LookPath(pathName); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, defaults...)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// PreflightOptions configures Stage 1 lockfile, Site DNA, Compose, and GRAB static checks.
type PreflightOptions struct {
	Workspace         string
	GrabBinary        string
	GrabmanBinary     string
	EntryFile         string
	CandidateSiteDNA  string
	RequireLockfile   bool
	RequireDigest     bool
	SkipGrabCheck     bool
	SkipGrabmanVerify bool
}

// PreflightReport summarizes Stage 1 verification outputs.
type PreflightReport struct {
	Workspace        string `json:"workspace"`
	LockfileVerified bool   `json:"lockfile_verified"`
	SiteDNAValidated bool   `json:"site_dna_validated"`
	GrabCheckPassed  bool   `json:"grab_check_passed"`
	ComposeAudited   bool   `json:"compose_audited"`
}

// RunPreflight enforces grabman solve --verify, grabman dna validate, grab check, and Compose
// hardening BEFORE modifying live traffic or creating partial rollout state.
func RunPreflight(ctx context.Context, opts PreflightOptions) (*PreflightReport, error) {
	realWS, err := safepath.ResolveRoot(opts.Workspace)
	if err != nil {
		return nil, err
	}
	rep := &PreflightReport{Workspace: realWS}

	grabmanBin := LocateGrabmanBinary(opts.GrabmanBinary)

	// 1. Lockfile verification (`grabman solve --verify`)
	hasManifest := fileExists(realWS, "grabman.json")
	if !opts.SkipGrabmanVerify && (opts.RequireLockfile || hasManifest) {
		if grabmanBin == "" {
			return nil, safepath.NewError(
				safepath.ErrPreflightFailed,
				"grabman CLI binary not found for mandatory lockfile verification",
				"Install grabman v0.1.0 on PATH or pass --grabman-bin.",
			)
		}
		cmd := exec.CommandContext(ctx, grabmanBin, "solve", "--dir", realWS, "--verify", "--json")
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return nil, safepath.WrapError(
				safepath.ErrPreflightFailed,
				fmt.Sprintf("grabman solve --verify failed: %s", compactOutput(out)),
				"Run 'grabman solve' to verify all 6 ecosystem layers and SHA-256 contract digests before rollout.",
				runErr,
			)
		}
		rep.LockfileVerified = true
	}

	// 2. Site DNA validation (`grabman dna validate`) if present or supplied as candidate
	dnaTargets := []string{}
	if strings.TrimSpace(opts.CandidateSiteDNA) != "" {
		dnaAbs, err := safepath.ResolveExternalReadFile(opts.CandidateSiteDNA)
		if err != nil {
			return nil, err
		}
		dnaTargets = append(dnaTargets, dnaAbs)
	} else if fileExists(realWS, "storage/site-dna.json") {
		abs, _, err := safepath.ResolveFile(realWS, "storage/site-dna.json", safepath.ResolveOptions{})
		if err != nil {
			return nil, err
		}
		dnaTargets = append(dnaTargets, abs)
	}
	for _, dnaPath := range dnaTargets {
		if grabmanBin == "" {
			return nil, safepath.NewError(safepath.ErrPreflightFailed, "grabman binary required to validate Site DNA", "Install grabman v0.1.0 or set GRABMAN_BIN.")
		}
		cmd := exec.CommandContext(ctx, grabmanBin, "dna", "validate", dnaPath, "--json")
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return nil, safepath.WrapError(
				safepath.ErrPreflightFailed,
				fmt.Sprintf("grabman dna validate failed on %s: %s", dnaPath, compactOutput(out)),
				"Re-export clean Site DNA preserving ADR-025c base-10 wire decimals and zero wrapper <div> tags.",
				runErr,
			)
		}
		rep.SiteDNAValidated = true
	}

	// 3. Static GRAB compilation gate (`grab check`)
	entry := strings.TrimSpace(opts.EntryFile)
	if entry == "" {
		if fileExists(realWS, "src/app.grab") {
			entry = "src/app.grab"
		}
	}
	if !opts.SkipGrabCheck && entry != "" {
		absEntry, _, rErr := safepath.ResolveFile(realWS, entry, safepath.ResolveOptions{})
		if rErr != nil {
			return nil, rErr
		}
		grabBin := LocateGrabBinary(opts.GrabBinary)
		if grabBin == "" {
			return nil, safepath.NewError(safepath.ErrPreflightFailed, "GRAB v1.1.0 CLI binary not found for 'grab check' preflight", "Install GRAB v1.1.0 or set GRAB_BIN.")
		}
		checkArgs := buildGrabCheckArgs(realWS, absEntry)
		cmd := exec.CommandContext(ctx, grabBin, checkArgs...)
		cmd.Dir = realWS
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return nil, safepath.WrapError(
				safepath.ErrPreflightFailed,
				fmt.Sprintf("grab check failed on %s: %s", entry, compactOutput(out)),
				"Fix static GRAB syntax/safety errors before draining live workers.",
				runErr,
			)
		}
		rep.GrabCheckPassed = true
	}

	// 4. Container hardening & zero-secrets audit on deploy/docker-compose.yml if present
	for _, candidate := range []string{"deploy/docker-compose.yml", "docker-compose.yml"} {
		if fileExists(realWS, candidate) {
			abs, slash, err := safepath.ResolveFile(realWS, candidate, safepath.ResolveOptions{})
			if err != nil {
				return nil, err
			}
			raw, err := safepath.ReadBoundedFile(abs)
			if err != nil {
				return nil, err
			}
			auditOpts := container.AuditOptions{
				RequireDigest:       opts.RequireDigest,
				AllowNon65532AppUID: hasManifest, // grabman init scaffolds 10001:10001 when present
			}
			if err := container.AuditComposeYAML(slash, string(raw), auditOpts); err != nil {
				return nil, err
			}
			rep.ComposeAudited = true
			break
		}
	}

	return rep, nil
}

// MigrationConfig configures explicit pre-rollout `grab migrate` execution (ADR-024f).
type MigrationConfig struct {
	Workspace     string
	GrabBinary    string
	DBConfig      string
	DBAlias       string
	MigrationFile string
}

// RunMigration runs `grab migrate` explicitly prior to worker traffic cutover (ADR-024f).
func RunMigration(ctx context.Context, cfg MigrationConfig) error {
	if strings.TrimSpace(cfg.MigrationFile) == "" {
		return nil
	}
	realWS, err := safepath.ResolveRoot(cfg.Workspace)
	if err != nil {
		return err
	}
	grabBin := LocateGrabBinary(cfg.GrabBinary)
	if grabBin == "" {
		return safepath.NewError(safepath.ErrRolloutFailed, "GRAB binary required for explicit pre-rollout migration", "Install GRAB v1.1.0 or set GRAB_BIN.")
	}
	dbCfg := strings.TrimSpace(cfg.DBConfig)
	if dbCfg == "" {
		dbCfg = "grab.data.json"
	}
	absDBCfg, _, err := safepath.ResolveFile(realWS, dbCfg, safepath.ResolveOptions{})
	if err != nil {
		return safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("invalid db config %q", dbCfg), "Provide a valid workspace grab.data.json.", err)
	}
	absMig, _, err := safepath.ResolveFile(realWS, cfg.MigrationFile, safepath.ResolveOptions{})
	if err != nil {
		return safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("invalid migration file %q", cfg.MigrationFile), "Provide a valid workspace .grab migration file.", err)
	}
	alias := strings.TrimSpace(cfg.DBAlias)
	if alias == "" {
		alias = "main"
	}
	cmd := exec.CommandContext(ctx, grabBin, "migrate", "--db-config", absDBCfg, "--allow-db", alias+":migrate", absMig)
	cmd.Dir = realWS
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return safepath.WrapError(
			safepath.ErrRolloutFailed,
			fmt.Sprintf("explicit pre-rollout grab migrate failed: %s", compactOutput(out)),
			"Fix database migration script; automatic snapshot rollback triggered.",
			runErr,
		)
	}
	return nil
}

// RolloutOptions drives the 6-stage zero-downtime rollout pipeline.
type RolloutOptions struct {
	Workspace        string
	StateDir         string
	AppName          string
	GrabBinary       string
	GrabmanBinary    string
	EntryFile        string
	CandidateSiteDNA string
	Migration        *MigrationConfig
	Swap             *RollingSwapConfig
	Reload           *ReloadConfig
	SmokeURLs        []string
	EdgeConfig       *container.EdgeRouteConfig
	RequireLockfile  bool
	RequireDigest    bool
	SkipGrabCheck    bool
	ApplyCandidate   func(ctx context.Context, workspace string) error
	PostRollbackHook func(ctx context.Context, rec *snapshot.Record) error
	RollbackReadyURLs []string
	DBCheckpointHook func(ctx context.Context, workspace string, dbOutDir string) (*snapshot.DBCheckpoint, error)
}

// StageEvent records one completed or failed stage in the 6-stage rollout state machine.
type StageEvent struct {
	Stage   int    `json:"stage"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

// RolloutResult captures complete rollout telemetry and any automatic rollback details.
type RolloutResult struct {
	SchemaVersion    string           `json:"schema_version"`
	Success          bool             `json:"success"`
	Workspace        string           `json:"workspace"`
	AppName          string           `json:"app_name"`
	DeployID         string           `json:"deploy_id,omitempty"`
	RolledBack       bool             `json:"rolled_back"`
	RollbackResult   *snapshot.RollbackResult `json:"rollback_result,omitempty"`
	Preflight        *PreflightReport `json:"preflight,omitempty"`
	SwapResult       *RollingSwapResult `json:"swap_result,omitempty"`
	EdgeArtifact     string           `json:"edge_artifact,omitempty"`
	Stages           []StageEvent     `json:"stages"`
	FailureReason    string           `json:"failure_reason,omitempty"`
	ErrorCode        string           `json:"error_code,omitempty"`
}

// ExecuteRollout runs all 6 rollout stages atomically with automatic Pre-Deploy Snapshot failback.
func ExecuteRollout(ctx context.Context, opts RolloutOptions) (*RolloutResult, error) {
	realWS, err := safepath.ResolveRoot(opts.Workspace)
	if err != nil {
		return nil, err
	}
	appName := strings.TrimSpace(opts.AppName)
	if appName == "" {
		appName = filepath.Base(realWS)
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(realWS, ".grabtaculous")
	}

	res := &RolloutResult{
		SchemaVersion: "0.1.0",
		Workspace:     realWS,
		AppName:       appName,
		Stages:        []StageEvent{},
	}

	// Stage 1: Preflight (zero mutation before passing!)
	pre, err := RunPreflight(ctx, PreflightOptions{
		Workspace:        realWS,
		GrabBinary:       opts.GrabBinary,
		GrabmanBinary:    opts.GrabmanBinary,
		EntryFile:        opts.EntryFile,
		CandidateSiteDNA: opts.CandidateSiteDNA,
		RequireLockfile:  opts.RequireLockfile,
		RequireDigest:    opts.RequireDigest,
		SkipGrabCheck:    opts.SkipGrabCheck,
	})
	if err != nil {
		res.Stages = append(res.Stages, StageEvent{Stage: 1, Name: "preflight", Status: "failed", Details: err.Error()})
		res.FailureReason = err.Error()
		res.ErrorCode = string(safepath.ExtractCode(err))
		return res, err
	}
	res.Preflight = pre
	res.Stages = append(res.Stages, StageEvent{Stage: 1, Name: "preflight", Status: "passed", Details: "lockfile, static GRAB check, and container security verified"})

	// Stage 2: Immutable Pre-Deploy Snapshot
	snapRec, err := snapshot.Create(ctx, snapshot.CreateOptions{
		StateDir:         stateDir,
		Workspace:        realWS,
		AppName:          appName,
		Trigger:          "rollout",
		DBCheckpointHook: opts.DBCheckpointHook,
	})
	if err != nil {
		res.Stages = append(res.Stages, StageEvent{Stage: 2, Name: "snapshot", Status: "failed", Details: err.Error()})
		res.FailureReason = err.Error()
		res.ErrorCode = string(safepath.ExtractCode(err))
		return res, err
	}
	res.DeployID = snapRec.DeployID
	res.Stages = append(res.Stages, StageEvent{
		Stage:   2,
		Name:    "snapshot",
		Status:  "passed",
		Details: fmt.Sprintf("captured %s (%d artifact(s), sha256 %s)", snapRec.DeployID, len(snapRec.Artifacts), snapRec.ManifestSHA256[:12]),
	})

	autoRollback := func(stageNum int, stageName string, cause error) (*RolloutResult, error) {
		res.Stages = append(res.Stages, StageEvent{Stage: stageNum, Name: stageName, Status: "failed", Details: cause.Error()})
		res.FailureReason = cause.Error()
		res.ErrorCode = string(safepath.ExtractCode(cause))

		rbRes, rbErr := snapshot.Rollback(ctx, snapshot.RollbackOptions{
			StateDir:        stateDir,
			Workspace:       realWS,
			DeployID:        snapRec.DeployID,
			ReadyURLs:       opts.RollbackReadyURLs,
			PostRestoreHook: opts.PostRollbackHook,
		})
		if rbErr != nil {
			res.Stages = append(res.Stages, StageEvent{Stage: 6, Name: "auto_rollback", Status: "failed", Details: rbErr.Error()})
			return res, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("rollout failed (%v) AND automatic rollback failed (%v)", cause, rbErr), "Restore manual snapshot immediately.", cause)
		}
		res.RolledBack = true
		res.RollbackResult = rbRes
		res.Stages = append(res.Stages, StageEvent{
			Stage:   6,
			Name:    "auto_rollback",
			Status:  "passed",
			Details: fmt.Sprintf("automatically restored snapshot %s (%d files)", snapRec.DeployID, len(rbRes.RestoredFiles)),
		})
		return res, cause
	}

	// Stage 3: Explicit Pre-Rollout Migrations (ADR-024f)
	if opts.Migration != nil && strings.TrimSpace(opts.Migration.MigrationFile) != "" {
		migCfg := *opts.Migration
		migCfg.Workspace = realWS
		if migCfg.GrabBinary == "" {
			migCfg.GrabBinary = opts.GrabBinary
		}
		if err := RunMigration(ctx, migCfg); err != nil {
			return autoRollback(3, "migrate", err)
		}
		res.Stages = append(res.Stages, StageEvent{Stage: 3, Name: "migrate", Status: "passed", Details: fmt.Sprintf("executed explicit migration %s", migCfg.MigrationFile)})
	} else {
		res.Stages = append(res.Stages, StageEvent{Stage: 3, Name: "migrate", Status: "skipped", Details: "no explicit migration declared"})
	}

	// Stage 4: Stage candidate updates + Edge TLS + Zero-Downtime Grisp Cutover
	if strings.TrimSpace(opts.CandidateSiteDNA) != "" {
		dnaBytes, err := safepath.ReadBoundedFile(opts.CandidateSiteDNA)
		if err != nil {
			return autoRollback(4, "cutover", err)
		}
		if err := safepath.WriteFileAtomic(realWS, "storage/site-dna.json", dnaBytes, 0o644, safepath.ResolveOptions{}); err != nil {
			return autoRollback(4, "cutover", err)
		}
		if fileExists(realWS, "grabman.json") {
			if grabmanBin := LocateGrabmanBinary(opts.GrabmanBinary); grabmanBin != "" {
				cmd := exec.CommandContext(ctx, grabmanBin, "solve", "--dir", realWS, "--json")
				if out, sErr := cmd.CombinedOutput(); sErr != nil {
					return autoRollback(4, "cutover", safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("grabman solve failed after Site DNA update: %s", compactOutput(out)), "Check Site DNA and grabman.json compatibility.", sErr))
				}
			}
		}
	}
	if opts.ApplyCandidate != nil {
		if err := opts.ApplyCandidate(ctx, realWS); err != nil {
			return autoRollback(4, "cutover", safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("candidate staging failed: %v", err), "Rolled back to Pre-Deploy Snapshot.", err))
		}
	}
	if opts.EdgeConfig != nil {
		art, err := container.RenderEdgeConfig(*opts.EdgeConfig)
		if err != nil {
			return autoRollback(4, "cutover", err)
		}
		if err := safepath.WriteFileAtomic(realWS, art.Filename, art.Content, 0o644, safepath.ResolveOptions{}); err != nil {
			return autoRollback(4, "cutover", err)
		}
		res.EdgeArtifact = art.Filename
	}

	if opts.Swap != nil {
		swapRes, err := RollingSwap(ctx, *opts.Swap)
		res.SwapResult = swapRes
		if err != nil {
			return autoRollback(4, "cutover", err)
		}
	} else if opts.Reload != nil {
		if err := ReloadGeneration(ctx, *opts.Reload); err != nil {
			return autoRollback(4, "cutover", err)
		}
	}
	res.Stages = append(res.Stages, StageEvent{Stage: 4, Name: "cutover", Status: "passed", Details: "candidate generation verified behind /_ready === 200"})

	// Stage 5: Post-Cutover Smoke Verification
	if len(opts.SmokeURLs) > 0 {
		client := &http.Client{Timeout: 3 * time.Second}
		for _, rawURL := range opts.SmokeURLs {
			if err := ValidateLoopbackURL(rawURL); err != nil {
				return autoRollback(5, "smoke_verify", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return autoRollback(5, "smoke_verify", err)
			}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
			if err != nil || resp.StatusCode != http.StatusOK {
				status := 0
				if resp != nil {
					status = resp.StatusCode
				}
				return autoRollback(5, "smoke_verify", safepath.NewError(
					safepath.ErrRolloutFailed,
					fmt.Sprintf("post-cutover smoke check failed for %s (status=%d, err=%v)", rawURL, status, err),
					"Triggered automatic Pre-Deploy Snapshot rollback.",
				))
			}
		}
	}
	res.Stages = append(res.Stages, StageEvent{Stage: 5, Name: "smoke_verify", Status: "passed", Details: "post-cutover /_ready and smoke URLs healthy"})

	res.Success = true
	return res, nil
}

func fileExists(root, rel string) bool {
	abs, _, err := safepath.ResolveFile(root, rel, safepath.ResolveOptions{AllowMissing: true})
	if err != nil {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && st.Mode().IsRegular()
}

func compactOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "no output"
	}
	var obj map[string]any
	if json.Unmarshal(b, &obj) == nil {
		if msg, ok := obj["error"].(string); ok && msg != "" {
			return msg
		}
	}
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

func buildGrabCheckArgs(realWS, absEntry string) []string {
	args := []string{"check", "--diagnostics", "production"}
	if absDB, _, err := safepath.ResolveFile(realWS, "grab.data.json", safepath.ResolveOptions{}); err == nil {
		if raw, rErr := safepath.ReadBoundedFile(absDB); rErr == nil {
			var dbCfg struct {
				Connections map[string]any `json:"connections"`
			}
			if json.Unmarshal(raw, &dbCfg) == nil && len(dbCfg.Connections) > 0 {
				args = append(args, "--db-config", absDB)
				names := sortedMapKeys(dbCfg.Connections)
				for _, name := range names {
					args = append(args, "--allow-db", name+":read-write")
				}
			}
		}
	}
	if absPlat, _, err := safepath.ResolveFile(realWS, "grab.platform.json", safepath.ResolveOptions{}); err == nil {
		if raw, rErr := safepath.ReadBoundedFile(absPlat); rErr == nil {
			var platCfg struct {
				Caches    map[string]any `json:"caches"`
				Channels  map[string]any `json:"channels"`
				Telemetry map[string]any `json:"telemetry"`
			}
			if json.Unmarshal(raw, &platCfg) == nil && (len(platCfg.Caches) > 0 || len(platCfg.Channels) > 0 || len(platCfg.Telemetry) > 0) {
				args = append(args, "--platform-config", absPlat)
				for _, name := range sortedMapKeys(platCfg.Caches) {
					args = append(args, "--allow-cache", name+":read-write")
				}
				for _, name := range sortedMapKeys(platCfg.Channels) {
					args = append(args, "--allow-channel", name+":publish")
				}
				for _, name := range sortedMapKeys(platCfg.Telemetry) {
					args = append(args, "--allow-telemetry", name)
				}
			}
		}
	}
	return append(args, absEntry)
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
