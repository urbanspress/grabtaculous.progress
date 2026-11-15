package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

func TestPreDeploySnapshotTamperDetectionAndAtomicRollback(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	stateDir := filepath.Join(ws, ".grabtaculous")

	// Populate representative workspace artifacts (lockfile, compose, edge, static grex, Site DNA, src, SQLite DB)
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("grabman.lock", `{"lock_version":"1","lock_digest_sha256":"abc"}`)
	mustWrite("deploy/docker-compose.yml", validHardenedCompose())
	mustWrite("deploy/Caddyfile", "example.com { reverse_proxy 127.0.0.1:8765 }\n")
	mustWrite("public/grex/tokens.css", ":root{--grex-primary:#047857;}\n")
	mustWrite("storage/site-dna.json", `{"price":"19.99"}`)
	mustWrite("storage/shop.db", "SQLITE-V1-BYTES")
	mustWrite("src/app.grab", "use server\n")
	// Sensitive .env must never be captured inside snapshot payload
	mustWrite(".env", "GRABLAB_MASTER_KEY=never_snapshot_me\n")

	fixedNow := func() time.Time {
		return time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	}
	rec, err := snapshot.Create(ctx, snapshot.CreateOptions{
		StateDir:  stateDir,
		Workspace: ws,
		AppName:   "test-shop",
		Trigger:   "deploy",
		DeployID:  "dep-v1",
		Now:       fixedNow,
	})
	if err != nil {
		t.Fatalf("snapshot.Create failed: %v", err)
	}
	if rec.DBCheckpoint == nil || rec.DBCheckpoint.Status != "captured" {
		t.Fatalf("expected SQLite DB checkpoint capture, got: %+v", rec.DBCheckpoint)
	}
	for _, art := range rec.Artifacts {
		if strings.Contains(art.RelPath, ".env") {
			t.Fatalf("snapshot leaked sensitive file %q", art.RelPath)
		}
	}

	// Immutable duplicate deploy_id rejected
	if _, err := snapshot.Create(ctx, snapshot.CreateOptions{
		StateDir:  stateDir,
		Workspace: ws,
		DeployID:  "dep-v1",
	}); safepath.ExtractCode(err) != safepath.ErrSnapshotFailed {
		t.Fatalf("expected GRABTAC-E701 for duplicate deploy_id, got: %v", err)
	}

	// Verify clean snapshot passes
	if _, err := snapshot.Verify(stateDir, "dep-v1"); err != nil {
		t.Fatalf("snapshot.Verify failed on untampered snapshot: %v", err)
	}

	// Mutate live workspace to simulate failed rollout (modified files + extra candidate file + mutated DB)
	mustWrite("public/grex/tokens.css", ":root{--grex-primary:#dc2626;}\n")
	mustWrite("public/grex/candidate-only.css", "body{background:red;}\n")
	mustWrite("storage/site-dna.json", `{"price":"999.00"}`)
	mustWrite("storage/shop.db", "CORRUPTED-V2-DB")

	// Start a healthy worker endpoint for post-rollback /_ready verification
	readyWorker := startMockGrispWorker(t, "v1", true)
	defer readyWorker.server.Close()

	rbRes, err := snapshot.Rollback(ctx, snapshot.RollbackOptions{
		StateDir:  stateDir,
		Workspace: ws,
		DeployID:  "dep-v1",
		ReadyURLs: []string{readyWorker.slot.BaseURL + "/_ready"},
	})
	if err != nil {
		t.Fatalf("snapshot.Rollback failed: %v", err)
	}
	if !rbRes.Restored || !rbRes.DBRestored || !rbRes.ReadyVerified {
		t.Fatalf("unexpected RollbackResult: %+v", rbRes)
	}

	// Verify exact byte-for-byte restoration and removal of candidate-only.css
	if b, _ := os.ReadFile(filepath.Join(ws, "public/grex/tokens.css")); string(b) != ":root{--grex-primary:#047857;}\n" {
		t.Fatalf("tokens.css not restored: %q", string(b))
	}
	if _, statErr := os.Stat(filepath.Join(ws, "public/grex/candidate-only.css")); !os.IsNotExist(statErr) {
		t.Fatalf("expected candidate-only.css to be removed on rollback")
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "storage/shop.db")); string(b) != "SQLITE-V1-BYTES" {
		t.Fatalf("SQLite DB not restored: %q", string(b))
	}

	// Tamper with payload inside snapshot and confirm Verify & Rollback fail closed with GRABTAC-E701
	tamperTarget := filepath.Join(stateDir, "snapshots", "dep-v1", "payload", "public", "grex", "tokens.css")
	if err := os.WriteFile(tamperTarget, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Verify(stateDir, "dep-v1"); safepath.ExtractCode(err) != safepath.ErrSnapshotFailed {
		t.Fatalf("expected GRABTAC-E701 on tampered snapshot payload, got: %v", err)
	}
	if _, err := snapshot.Rollback(ctx, snapshot.RollbackOptions{
		StateDir:  stateDir,
		Workspace: ws,
		DeployID:  "dep-v1",
	}); safepath.ExtractCode(err) != safepath.ErrSnapshotFailed {
		t.Fatalf("expected Rollback to refuse tampered snapshot with GRABTAC-E701, got: %v", err)
	}
}
