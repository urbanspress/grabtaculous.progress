package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"grabtaculous/internal/catalog"
	"grabtaculous/internal/compose"
	"grabtaculous/internal/container"
	"grabtaculous/internal/daemon"
	"grabtaculous/internal/edge"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

// Version identifies the release version (overridable via -ldflags "-X main.Version=...").
var Version = "1.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	sub := strings.TrimSpace(args[0])
	rest := args[1:]

	switch sub {
	case "version", "-v", "--version":
		return cmdVersion(rest, stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "daemon":
		return cmdDaemon(rest, stdout, stderr)
	case "catalog":
		return cmdCatalog(rest, stdout, stderr)
	case "deploy":
		return cmdDeploy(rest, stdout, stderr)
	case "rollout":
		return cmdRollout(rest, stdout, stderr)
	case "rollback":
		return cmdRollback(rest, stdout, stderr)
	case "snapshot":
		return cmdSnapshot(rest, stdout, stderr)
	case "audit":
		return cmdAudit(rest, stdout, stderr)
	case "status":
		return cmdStatus(rest, stdout, stderr)
	case "edge":
		return cmdEdge(rest, stdout, stderr)
	case "profile":
		return cmdProfile(rest, stdout, stderr)
	case "migrate-export":
		return cmdMigrateExport(rest, stdout, stderr)
	case "migrate-import":
		return cmdMigrateImport(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Error: unknown command %q\n\n", sub)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `grabtaculous v`+Version+` — Multi-Server Supervisor, --read-only Orchestrator & Zero-Downtime Rollout Engine

Usage:
  grabtaculous daemon   [--listen 127.0.0.1:9440] [--socket <unix-path>] [--state-dir <dir>] [--catalog-dir <dir>] [--json]
  grabtaculous catalog  <list|inspect <id>> [--catalog-dir <dir>] [--json]
  grabtaculous deploy   <blueprint-id> [--dir <path>] [--name <app>] [--domain <domain>] [--edge-proxy caddy|nginx|cloudflare-tunnel] [--workers <n>] [--state-dir <dir>] [--catalog-dir <dir>] [--grabman-bin <path>] [--json]
  grabtaculous rollout  [<dir>] [--dir <path>] [--name <app>] [--entry <file.grab>] [--candidate-dna <file.json>] [--migrate-file <migrations.grab>] [--db-config <grab.data.json>] [--db-alias <alias>] [--edge-proxy caddy|nginx|cloudflare-tunnel] [--domain <domain>] [--grab-bin <path>] [--grabman-bin <path>] [--state-dir <dir>] [--json]
  grabtaculous rollback <deploy-id> [--dir <path>] [--state-dir <dir>] [--ready-url <url>] [--json]
  grabtaculous snapshot <list|create|verify <deploy-id>> [--dir <path>] [--name <app>] [--state-dir <dir>] [--json]
  grabtaculous audit          [--dir <path>] [--file <compose.yml>] [--require-digest] [--json]
  grabtaculous status         [--dir <path>] [--state-dir <dir>] [--json]
  grabtaculous edge           <bootstrap-plan|render> [--profile vps-8c-16g] [--domain <domain>] [--wp-domain <domain>] [--out-dir <dir>] [--json]
  grabtaculous profile        <list|render <id>> [--domain <domain>] [--out-dir <dir>] [--json]
  grabtaculous migrate-export [--dir <ws>] [--out <bundle>] [--source-profile vps-6c-12g] [--target-profile vps-8c-16g] [--pg-dump <file>] [--mariadb-dump <file>] [--uploads-dir <dir>] [--grex-dir <dir>] [--json]
  grabtaculous migrate-import --bundle <bundle> [--dir <ws>] [--target-profile vps-8c-16g] [--uploads-dir <dir>] [--grex-dir <dir>] [--verify-only] [--json]
  grabtaculous version        [--json]
`)
}

func cmdVersion(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			fmt.Fprintf(stderr, "Error: unknown flag %q for version\n", a)
			return 2
		}
	}
	if asJSON {
		writeJSON(stdout, map[string]any{
			"name":           "grabtaculous",
			"version":        Version,
			"schema_version": "1.0.0",
			"default_bind":   daemon.DefaultListenAddr,
		})
		return 0
	}
	fmt.Fprintf(stdout, "grabtaculous v%s\n", Version)
	return 0
}

func cmdDaemon(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"listen":      true,
		"socket":      true,
		"state-dir":   true,
		"catalog-dir": true,
	})
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	listenAddr := fs.String("listen", daemon.DefaultListenAddr, "Strictly 127.0.0.1:<port> or [::1]:<port> bind address")
	sockPath := fs.String("socket", "", "Optional Unix domain socket path")
	stateDir := fs.String("state-dir", ".grabtaculous", "Supervisor state directory")
	catalogDir := fs.String("catalog-dir", "", "Optional external curated catalog directory")
	asJSON := fs.Bool("json", false, "Output startup info as JSON")

	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	bindTarget := strings.TrimSpace(*listenAddr)
	if strings.TrimSpace(*sockPath) != "" {
		bindTarget = "unix:" + strings.TrimSpace(*sockPath)
	}

	srv, err := daemon.NewServer(daemon.ServerConfig{
		ListenAddr: bindTarget,
		StateDir:   *stateDir,
		CatalogDir: *catalogDir,
	})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}

	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":      true,
			"service": "grabtaculousd",
			"version": Version,
			"listen":  bindTarget,
			"state":   *stateDir,
		})
	} else {
		fmt.Fprintf(stdout, "Starting grabtaculousd v%s on %s (ADR-005 loopback/socket-only; token stored in %s)\n",
			Version, bindTarget, filepath.Join(*stateDir, "control.token"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	return 0
}

func cmdCatalog(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: grabtaculous catalog <list|inspect <id>> [--catalog-dir <dir>] [--json]")
		return 2
	}
	sub := strings.TrimSpace(args[0])
	rest := reorderFlags(args[1:], map[string]bool{
		"catalog-dir": true,
	})

	switch sub {
	case "list":
		fs := flag.NewFlagSet("catalog list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		catalogDir := fs.String("catalog-dir", "", "Optional catalog directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		loader := catalog.NewLoader(*catalogDir)
		items, err := loader.List()
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":         true,
				"blueprints": items,
			})
			return 0
		}
		fmt.Fprintf(stdout, "Curated 1-Click App Marketplace (%d verified blueprint(s)):\n", len(items))
		for _, item := range items {
			fmt.Fprintf(stdout, "  %-22s %-10s v%-8s sha256:%s  %s\n", item.ID, item.Category, item.Version, item.SHA256[:12], item.Name)
		}
		return 0

	case "inspect":
		fs := flag.NewFlagSet("catalog inspect", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		catalogDir := fs.String("catalog-dir", "", "Optional catalog directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		if fs.NArg() != 1 {
			return writeError(*asJSON, stdout, stderr, fmt.Errorf("usage: grabtaculous catalog inspect <blueprint-id> [--catalog-dir <dir>] [--json]"), 2)
		}
		loader := catalog.NewLoader(*catalogDir)
		bp, err := loader.LoadBlueprint(fs.Arg(0))
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":        true,
				"blueprint": bp,
			})
			return 0
		}
		fmt.Fprintf(stdout, "%s (v%s) [%s]\n", bp.Name, bp.Version, bp.ID)
		fmt.Fprintf(stdout, "  Summary:  %s\n", bp.Summary)
		fmt.Fprintf(stdout, "  SHA-256:  %s\n", bp.VerifiedManifestSHA256)
		fmt.Fprintf(stdout, "  Services: %d (all --read-only, cap_drop ALL, non-root UID 65532)\n", len(bp.Stack.Services))
		for _, svc := range bp.Stack.Services {
			fmt.Fprintf(stdout, "    - %s -> %s (ports: %v)\n", svc.Name, svc.Image, svc.Ports)
		}
		return 0

	default:
		fmt.Fprintf(stderr, "Error: unknown catalog subcommand %q (expected list or inspect)\n", sub)
		return 2
	}
}

func cmdDeploy(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":         true,
		"name":        true,
		"domain":      true,
		"edge-proxy":  true,
		"workers":     true,
		"state-dir":   true,
		"catalog-dir": true,
		"grabman-bin": true,
	})
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Target deployment workspace directory")
	name := fs.String("name", "", "Optional application display/stack name")
	domain := fs.String("domain", "localhost", "Edge proxy domain name")
	edgeProxy := fs.String("edge-proxy", "caddy", "Edge proxy provider (caddy, nginx, cloudflare-tunnel)")
	workers := fs.Int("workers", 0, "Worker port slots to allocate in 127.0.0.1:8765-8772")
	stateDir := fs.String("state-dir", "", "Supervisor state/snapshot directory")
	catalogDir := fs.String("catalog-dir", "", "Optional external curated catalog directory")
	grabmanBin := fs.String("grabman-bin", "", "Optional explicit path to grabman CLI")
	skipLock := fs.Bool("skip-lock-verify", false, "Skip grabman solve --verify even if grabman.json exists")
	asJSON := fs.Bool("json", false, "Output JSON")

	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	if fs.NArg() != 1 {
		return writeError(*asJSON, stdout, stderr, fmt.Errorf("usage: grabtaculous deploy <blueprint-id> [--dir <path>] [--edge-proxy caddy|nginx|cloudflare-tunnel] [--json]"), 2)
	}

	res, err := daemon.DeployBlueprint(context.Background(), daemon.DeployOptions{
		BlueprintID:    fs.Arg(0),
		TargetDir:      *dir,
		StateDir:       *stateDir,
		CatalogDir:     *catalogDir,
		AppName:        *name,
		Domain:         *domain,
		EdgeProxy:      *edgeProxy,
		Workers:        *workers,
		GrabmanBinary:  *grabmanBin,
		SkipLockVerify: *skipLock,
	})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":     true,
			"deploy": res,
		})
		return 0
	}
	fmt.Fprintf(stdout, "Deployed curated blueprint %s (v%s) to %s\n", res.BlueprintID, res.BlueprintVersion, res.Workspace)
	fmt.Fprintf(stdout, "  Snapshot ID: %s\n", res.DeploySnapshotID)
	fmt.Fprintf(stdout, "  Edge Proxy:  %s (%s -> %s)\n", res.EdgeProxy, res.Domain, res.EdgeFile)
	fmt.Fprintf(stdout, "  Loopback:    127.0.0.1 ports %v\n", res.WorkerPorts)
	fmt.Fprintf(stdout, "  Compose:     %s\n", res.ComposeFile)
	return 0
}

func cmdRollout(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":           true,
		"name":          true,
		"entry":         true,
		"domain":        true,
		"edge-proxy":    true,
		"candidate-dna": true,
		"migrate-file":  true,
		"db-config":     true,
		"db-alias":      true,
		"grab-bin":      true,
		"grabman-bin":   true,
		"state-dir":     true,
		"smoke-url":     true,
	})
	fs := flag.NewFlagSet("rollout", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "Target workspace directory")
	name := fs.String("name", "", "Application name")
	entry := fs.String("entry", "", "Optional GRAB entry file relative path")
	domain := fs.String("domain", "", "Optional domain for updating edge proxy config")
	edgeProxy := fs.String("edge-proxy", "", "Optional edge proxy (caddy, nginx, cloudflare-tunnel)")
	candidateDNA := fs.String("candidate-dna", "", "Optional Site DNA JSON file to validate and apply")
	migrateFile := fs.String("migrate-file", "", "Optional explicit .grab migration file to run before traffic switch")
	dbConfig := fs.String("db-config", "grab.data.json", "Workspace database config file")
	dbAlias := fs.String("db-alias", "main", "Database alias for explicit grab migrate")
	grabBin := fs.String("grab-bin", "", "Optional GRAB CLI path")
	grabmanBin := fs.String("grabman-bin", "", "Optional grabman CLI path")
	stateDir := fs.String("state-dir", "", "Supervisor state/snapshot directory")
	smokeURL := fs.String("smoke-url", "", "Optional loopback smoke URL to verify post-cutover")
	requireLock := fs.Bool("require-lock", false, "Require grabman.lock verification")
	skipGrab := fs.Bool("skip-grab-check", false, "Skip grab check preflight")
	asJSON := fs.Bool("json", false, "Output JSON")

	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	targetDir := strings.TrimSpace(*dir)
	if fs.NArg() == 1 && targetDir == "" {
		targetDir = fs.Arg(0)
	} else if fs.NArg() > 1 || (fs.NArg() == 1 && targetDir != "") {
		return writeError(*asJSON, stdout, stderr, fmt.Errorf("unexpected positional arguments: %v", fs.Args()), 2)
	}
	if targetDir == "" {
		targetDir = "."
	}

	var mig *rollout.MigrationConfig
	if strings.TrimSpace(*migrateFile) != "" {
		mig = &rollout.MigrationConfig{
			MigrationFile: *migrateFile,
			DBConfig:      *dbConfig,
			DBAlias:       *dbAlias,
			GrabBinary:    *grabBin,
		}
	}
	var edgeCfg *container.EdgeRouteConfig
	if strings.TrimSpace(*edgeProxy) != "" || strings.TrimSpace(*domain) != "" {
		edgeCfg = &container.EdgeRouteConfig{
			EdgeProxy:     *edgeProxy,
			Domain:        *domain,
			UpstreamHost:  "127.0.0.1",
			UpstreamPorts: []int{rollout.DefaultWorkerBasePort},
			HealthPath:    "/_ready",
		}
	}
	var smoke []string
	if strings.TrimSpace(*smokeURL) != "" {
		smoke = []string{strings.TrimSpace(*smokeURL)}
	}

	res, err := rollout.ExecuteRollout(context.Background(), rollout.RolloutOptions{
		Workspace:        targetDir,
		StateDir:         *stateDir,
		AppName:          *name,
		GrabBinary:       *grabBin,
		GrabmanBinary:    *grabmanBin,
		EntryFile:        *entry,
		CandidateSiteDNA: *candidateDNA,
		Migration:        mig,
		EdgeConfig:       edgeCfg,
		SmokeURLs:        smoke,
		RequireLockfile:  *requireLock,
		SkipGrabCheck:    *skipGrab,
	})
	if err != nil {
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":      false,
				"code":    string(safepath.ExtractCode(err)),
				"error":   err.Error(),
				"rollout": res,
			})
			return 1
		}
		return writeError(false, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":      true,
			"rollout": res,
		})
		return 0
	}
	fmt.Fprintf(stdout, "Zero-downtime rollout succeeded for %s\n", res.AppName)
	fmt.Fprintf(stdout, "  Pre-Deploy Snapshot: %s\n", res.DeployID)
	for _, st := range res.Stages {
		fmt.Fprintf(stdout, "  Stage %d (%s): %s — %s\n", st.Stage, st.Name, st.Status, st.Details)
	}
	return 0
}

func cmdRollback(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":       true,
		"state-dir": true,
		"ready-url": true,
	})
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Target workspace directory")
	stateDir := fs.String("state-dir", "", "Supervisor state/snapshot directory")
	readyURL := fs.String("ready-url", "", "Optional loopback /_ready URL to verify after rollback")
	asJSON := fs.Bool("json", false, "Output JSON")

	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	if fs.NArg() != 1 {
		return writeError(*asJSON, stdout, stderr, fmt.Errorf("usage: grabtaculous rollback <deploy_id> [--dir <path>] [--state-dir <dir>] [--json]"), 2)
	}
	var readyURLs []string
	if strings.TrimSpace(*readyURL) != "" {
		readyURLs = []string{strings.TrimSpace(*readyURL)}
	}
	res, err := snapshot.Rollback(context.Background(), snapshot.RollbackOptions{
		StateDir:  *stateDir,
		Workspace: *dir,
		DeployID:  fs.Arg(0),
		ReadyURLs: readyURLs,
	})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":       true,
			"rollback": res,
		})
		return 0
	}
	fmt.Fprintf(stdout, "Rolled back %s cleanly from snapshot %s\n", res.Workspace, res.DeployID)
	fmt.Fprintf(stdout, "  Restored files: %d\n", len(res.RestoredFiles))
	fmt.Fprintf(stdout, "  Removed files:  %d\n", len(res.RemovedFiles))
	return 0
}

func cmdSnapshot(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: grabtaculous snapshot <list|create|verify <deploy_id>> [--dir <path>] [--state-dir <dir>] [--json]")
		return 2
	}
	sub := strings.TrimSpace(args[0])
	rest := reorderFlags(args[1:], map[string]bool{
		"dir":       true,
		"name":      true,
		"state-dir": true,
	})

	switch sub {
	case "list":
		fs := flag.NewFlagSet("snapshot list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", ".", "Workspace directory")
		stateDir := fs.String("state-dir", "", "Snapshot state directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		sDir := resolveCLIStateDir(*stateDir, *dir)
		items, err := snapshot.List(sDir)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":        true,
				"snapshots": items,
			})
			return 0
		}
		fmt.Fprintf(stdout, "Snapshots (%d) in %s:\n", len(items), sDir)
		for _, it := range items {
			fmt.Fprintf(stdout, "  %s  %-10s  %2d files  sha256:%s  (%s)\n", it.DeployID, it.Trigger, it.ArtifactCount, it.ManifestSHA256[:12], it.CreatedAtUTC)
		}
		return 0

	case "create":
		fs := flag.NewFlagSet("snapshot create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", ".", "Workspace directory")
		name := fs.String("name", "", "App name")
		stateDir := fs.String("state-dir", "", "Snapshot state directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		rec, err := snapshot.Create(context.Background(), snapshot.CreateOptions{
			StateDir:  *stateDir,
			Workspace: *dir,
			AppName:   *name,
			Trigger:   "manual",
		})
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":       true,
				"snapshot": rec,
			})
			return 0
		}
		fmt.Fprintf(stdout, "Created immutable snapshot %s (%d artifact(s), manifest sha256:%s)\n",
			rec.DeployID, len(rec.Artifacts), rec.ManifestSHA256)
		return 0

	case "verify":
		fs := flag.NewFlagSet("snapshot verify", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", ".", "Workspace directory")
		stateDir := fs.String("state-dir", "", "Snapshot state directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		if fs.NArg() != 1 {
			return writeError(*asJSON, stdout, stderr, fmt.Errorf("usage: grabtaculous snapshot verify <deploy_id> [--dir <path>] [--state-dir <dir>] [--json]"), 2)
		}
		sDir := resolveCLIStateDir(*stateDir, *dir)
		rec, err := snapshot.Verify(sDir, fs.Arg(0))
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"ok":       true,
				"verified": true,
				"snapshot": rec,
			})
			return 0
		}
		fmt.Fprintf(stdout, "Snapshot %s VERIFIED (%d artifact(s), manifest sha256:%s)\n",
			rec.DeployID, len(rec.Artifacts), rec.ManifestSHA256)
		return 0

	default:
		fmt.Fprintf(stderr, "Error: unknown snapshot subcommand %q (expected list, create, or verify)\n", sub)
		return 2
	}
}

func cmdAudit(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":  true,
		"file": true,
	})
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Workspace directory")
	file := fs.String("file", "deploy/docker-compose.yml", "Compose file relative path")
	reqDigest := fs.Bool("require-digest", false, "Require @sha256 digest pins on every image")
	asJSON := fs.Bool("json", false, "Output JSON")

	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	realWS, err := safepath.ResolveRoot(*dir)
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	abs, slash, err := safepath.ResolveFile(realWS, *file, safepath.ResolveOptions{})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	raw, err := safepath.ReadBoundedFile(abs)
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if err := container.AuditComposeYAML(slash, string(raw), container.AuditOptions{RequireDigest: *reqDigest}); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":   true,
			"file": slash,
		})
		return 0
	}
	fmt.Fprintf(stdout, "%s passed GRABTAC-E401 --read-only hardening & zero-secrets audit.\n", slash)
	return 0
}

func cmdStatus(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":       true,
		"state-dir": true,
	})
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Workspace directory")
	stateDir := fs.String("state-dir", "", "Supervisor state directory")
	asJSON := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	sDir := resolveCLIStateDir(*stateDir, *dir)
	store, err := daemon.NewInventoryStore(sDir)
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	doc, err := store.Snapshot()
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	snaps, _ := snapshot.List(sDir)
	if *asJSON {
		writeJSON(stdout, map[string]any{
			"ok":             true,
			"version":        Version,
			"state_dir":      sDir,
			"node_count":     len(doc.Nodes),
			"app_count":      len(doc.Apps),
			"snapshot_count": len(snaps),
			"inventory":      doc,
		})
		return 0
	}
	fmt.Fprintf(stdout, "Grabtaculous v%s Status (%s)\n", Version, sDir)
	fmt.Fprintf(stdout, "  Nodes:     %d\n", len(doc.Nodes))
	fmt.Fprintf(stdout, "  Apps:      %d\n", len(doc.Apps))
	fmt.Fprintf(stdout, "  Snapshots: %d\n", len(snaps))
	return 0
}

func resolveCLIStateDir(stateDir, workspace string) string {
	if strings.TrimSpace(stateDir) != "" {
		return strings.TrimSpace(stateDir)
	}
	return filepath.Join(workspace, ".grabtaculous")
}

func reorderFlags(args []string, valueFlags map[string]bool) []string {
	var flagsPart []string
	var posPart []string
	afterTerminator := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterTerminator {
			posPart = append(posPart, a)
			continue
		}
		if a == "--" {
			afterTerminator = true
			continue
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			flagsPart = append(flagsPart, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if valueFlags[name] && i+1 < len(args) {
					i++
					flagsPart = append(flagsPart, args[i])
				}
			}
			continue
		}
		posPart = append(posPart, a)
	}
	if afterTerminator {
		posPart = append([]string{"--"}, posPart...)
	}
	return append(flagsPart, posPart...)
}

func writeJSON(w io.Writer, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(w, `{"ok":false,"error":%q}`+"\n", err.Error())
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func writeError(asJSON bool, stdout, stderr io.Writer, err error, code int) int {
	diagCode := safepath.ExtractCode(err)
	var dErr *safepath.DiagError
	remediation := ""
	if errors.As(err, &dErr) {
		remediation = dErr.Remediation
	}
	if asJSON {
		payload := map[string]any{
			"ok":    false,
			"error": err.Error(),
		}
		if diagCode != "" {
			payload["code"] = string(diagCode)
		}
		if remediation != "" {
			payload["remediation"] = remediation
		}
		writeJSON(stdout, payload)
		return code
	}
	if diagCode != "" {
		fmt.Fprintf(stderr, "Error [%s]: %s\n", diagCode, err.Error())
	} else {
		fmt.Fprintf(stderr, "Error: %s\n", err.Error())
	}
	if remediation != "" {
		fmt.Fprintf(stderr, "Remediation: %s\n", remediation)
	}
	return code
}

func cmdEdge(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: grabtaculous edge <bootstrap-plan|render> [--profile vps-8c-16g] [--domain <domain>] [--wp-domain <domain>] [--out-dir <dir>] [--json]")
		return 2
	}
	sub := strings.TrimSpace(args[0])
	rest := reorderFlags(args[1:], map[string]bool{
		"profile":    true,
		"domain":     true,
		"wp-domain":  true,
		"out-dir":    true,
		"upload-dir": true,
	})
	switch sub {
	case "bootstrap-plan":
		fs := flag.NewFlagSet("edge bootstrap-plan", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		codename := fs.String("codename", "noble", "Ubuntu codename")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		plan := edge.BuildNginx130BootstrapPlan(edge.BootstrapOptions{DistroCodename: *codename, DryRun: true})
		if *asJSON {
			writeJSON(stdout, map[string]any{"ok": true, "plan": plan})
			return 0
		}
		fmt.Fprintf(stdout, "NGINX 1.30.6 Official Repository Plan (%s, PGP %s): %d structured-argv steps\n", plan.DistroCodename, plan.PinnedFingerprint, len(plan.Steps))
		return 0

	case "render":
		fs := flag.NewFlagSet("edge render", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		profileID := fs.String("profile", "vps-8c-16g", "Hardware profile ID")
		domain := fs.String("domain", "bookstore.example.com", "Primary Grisp domain")
		wpDomain := fs.String("wp-domain", "", "Optional WordPress companion domain")
		outDir := fs.String("out-dir", ".", "Target output directory")
		uploadDir := fs.String("upload-dir", "/var/lib/grisp/uploads/", "Local NVMe upload directory for X-Accel-Redirect")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		prof, err := compose.GetProfile(*profileID)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		opts := edge.DefaultNginxConfigOptions(*domain)
		opts.WorkerProcesses = prof.NginxWorkers
		opts.UpstreamPorts = prof.GrispPorts
		opts.UploadAliasDir = *uploadDir
		if strings.TrimSpace(*wpDomain) != "" {
			opts.WPDomain = strings.TrimSpace(*wpDomain)
		}
		mainConf, err := edge.RenderMainNginxConf(opts)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		grispConf, err := edge.RenderGrispSiteConf(opts)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		wpConf, err := edge.RenderWordPressFPMConf(opts)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		sysctlConf := edge.RenderQuicSysctlConf()

		root, err := safepath.InitRoot(*outDir)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		written := []string{
			"etc/nginx/nginx.conf",
			"etc/nginx/sites-available/grisp.conf",
			"etc/nginx/sites-available/wordpress-fpm.conf",
			"etc/sysctl.d/99-nginx-quic.conf",
		}
		contents := [][]byte{mainConf, grispConf, wpConf, sysctlConf}
		for i, rel := range written {
			if err := safepath.WriteFileAtomic(root, rel, contents[i], 0o644, safepath.ResolveOptions{}); err != nil {
				return writeError(*asJSON, stdout, stderr, err, 1)
			}
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"ok": true, "profile": prof.ID, "files_written": written})
			return 0
		}
		fmt.Fprintf(stdout, "Rendered NGINX 1.30.6 edge configuration for profile %s into %s (%d files)\n", prof.ID, root, len(written))
		return 0

	default:
		fmt.Fprintf(stderr, "Error: unknown edge subcommand %q (expected bootstrap-plan or render)\n", sub)
		return 2
	}
}

func cmdProfile(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: grabtaculous profile <list|render <id>> [--domain <domain>] [--out-dir <dir>] [--json]")
		return 2
	}
	sub := strings.TrimSpace(args[0])
	rest := reorderFlags(args[1:], map[string]bool{
		"domain":  true,
		"out-dir": true,
	})
	switch sub {
	case "list":
		fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		profiles := compose.ListProfiles()
		if *asJSON {
			writeJSON(stdout, map[string]any{"ok": true, "profiles": profiles})
			return 0
		}
		for _, p := range profiles {
			fmt.Fprintf(stdout, "  %-12s  %2d vCPU  %2d GB RAM  %3d GB NVMe  %d Grisp workers (%v)\n", p.ID, p.VCPU, p.RAMGB, p.NVMeGB, p.GrispWorkers, p.GrispPorts)
		}
		return 0

	case "render":
		fs := flag.NewFlagSet("profile render", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		domain := fs.String("domain", "bookstore.example.com", "Primary site domain")
		outDir := fs.String("out-dir", ".", "Target workspace directory")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 2)
		}
		if fs.NArg() != 1 {
			return writeError(*asJSON, stdout, stderr, fmt.Errorf("usage: grabtaculous profile render <profile-id> [--domain <domain>] [--out-dir <dir>] [--json]"), 2)
		}
		profID := fs.Arg(0)
		composeYAML, err := compose.RenderProfileCompose(profID, *domain)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		if err := container.AuditComposeYAML("deploy/docker-compose.yml", string(composeYAML), container.AuditOptions{RequireDigest: true}); err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		pgConf, err := compose.RenderPostgresConf(profID)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		reloadSh, err := compose.RenderReloadWorkersScript(profID)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		unitFile, err := compose.RenderGrispSystemdUnit(profID)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		backupSh, err := compose.RenderGrispBackupScript(profID)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		root, err := safepath.InitRoot(*outDir)
		if err != nil {
			return writeError(*asJSON, stdout, stderr, err, 1)
		}
		files := []struct {
			rel  string
			data []byte
			mode os.FileMode
		}{
			{"deploy/docker-compose.yml", composeYAML, 0o644},
			{"deploy/postgres/postgresql.conf", pgConf, 0o644},
			{"deploy/reload-workers.sh", reloadSh, 0o755},
			{"deploy/grisp-stack.service", unitFile, 0o644},
			{"deploy/grisp-backup.sh", backupSh, 0o755},
		}
		written := make([]string, 0, len(files))
		for _, f := range files {
			if err := safepath.WriteFileAtomic(root, f.rel, f.data, f.mode, safepath.ResolveOptions{}); err != nil {
				return writeError(*asJSON, stdout, stderr, err, 1)
			}
			written = append(written, f.rel)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"ok": true, "profile": profID, "files_written": written})
			return 0
		}
		fmt.Fprintf(stdout, "Rendered hardened %s stack templates into %s (%d files)\n", profID, root, len(written))
		return 0

	default:
		fmt.Fprintf(stderr, "Error: unknown profile subcommand %q (expected list or render)\n", sub)
		return 2
	}
}

func cmdMigrateExport(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"dir":            true,
		"out":            true,
		"source-profile": true,
		"target-profile": true,
		"uploads-dir":    true,
		"grex-dir":       true,
		"pg-dump":        true,
		"mariadb-dump":   true,
	})
	fs := flag.NewFlagSet("migrate-export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Source workspace directory")
	out := fs.String("out", "", "Output bundle directory or .tar.gz file")
	srcProf := fs.String("source-profile", "vps-6c-12g", "Source VPS hardware profile")
	dstProf := fs.String("target-profile", "vps-8c-16g", "Target VPS hardware profile")
	uploadsDir := fs.String("uploads-dir", "", "Optional uploads directory override")
	grexDir := fs.String("grex-dir", "", "Optional public/grex directory override")
	pgDump := fs.String("pg-dump", "", "Optional PostgreSQL 18 SQL dump path")
	mariadbDump := fs.String("mariadb-dump", "", "Optional MariaDB 11.8 SQL dump path")
	asJSON := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	man, err := snapshot.MigrateExport(context.Background(), snapshot.MigrateExportOptions{
		SourceWorkspace: *dir,
		OutputBundle:    *out,
		SourceProfile:   *srcProf,
		TargetProfile:   *dstProf,
		UploadsDir:      *uploadsDir,
		GrexPublicDir:   *grexDir,
		PostgresDump:    *pgDump,
		MariaDBDump:     *mariadbDump,
	})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"ok": true, "manifest": man})
		return 0
	}
	fmt.Fprintf(stdout, "Exported migration bundle %s (%s -> %s, %d files, sha256:%s)\n", man.BundleID, man.SourceProfile, man.TargetProfile, len(man.Artifacts), man.ManifestSHA256)
	return 0
}

func cmdMigrateImport(args []string, stdout, stderr io.Writer) int {
	reordered := reorderFlags(args, map[string]bool{
		"bundle":         true,
		"dir":            true,
		"target-profile": true,
		"uploads-dir":    true,
		"grex-dir":       true,
	})
	fs := flag.NewFlagSet("migrate-import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bundle := fs.String("bundle", "", "Input bundle directory or .tar.gz file")
	dir := fs.String("dir", ".", "Target workspace directory")
	dstProf := fs.String("target-profile", "vps-8c-16g", "Target VPS hardware profile")
	uploadsDir := fs.String("uploads-dir", "", "Optional uploads target directory override")
	grexDir := fs.String("grex-dir", "", "Optional public/grex target directory override")
	verifyOnly := fs.Bool("verify-only", false, "Verify SHA-256 digests and 15 GB NVMe reserve without modifying files")
	asJSON := fs.Bool("json", false, "Output JSON")
	if err := fs.Parse(reordered); err != nil {
		return writeError(*asJSON, stdout, stderr, err, 2)
	}
	res, err := snapshot.MigrateImport(context.Background(), snapshot.MigrateImportOptions{
		BundlePath:      *bundle,
		TargetWorkspace: *dir,
		TargetProfile:   *dstProf,
		UploadsDir:      *uploadsDir,
		GrexPublicDir:   *grexDir,
		VerifyOnly:      *verifyOnly,
	})
	if err != nil {
		return writeError(*asJSON, stdout, stderr, err, 1)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"ok": true, "import": res})
		return 0
	}
	fmt.Fprintf(stdout, "Verified & imported migration bundle %s -> %s (%d files, ready_for_cutover=%v)\n", res.BundleID, res.TargetProfile, res.ArtifactCount, res.ReadyForCutover)
	return 0
}
