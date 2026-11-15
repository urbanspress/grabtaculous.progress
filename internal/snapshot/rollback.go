package snapshot

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grabtaculous/internal/safepath"
)

// RollbackOptions configures atomic Pre-Deploy Snapshot restoration.
type RollbackOptions struct {
	StateDir        string
	Workspace       string
	DeployID        string
	ReadyURLs       []string
	ReadyTimeout    time.Duration
	PostRestoreHook func(ctx context.Context, rec *Record) error
}

// RollbackResult summarizes the outcome of a Pre-Deploy Snapshot restore.
type RollbackResult struct {
	SchemaVersion  string   `json:"schema_version"`
	DeployID       string   `json:"deploy_id"`
	Workspace      string   `json:"workspace"`
	Restored       bool     `json:"restored"`
	ReadyVerified  bool     `json:"ready_verified"`
	RestoredFiles  []string `json:"restored_files"`
	RemovedFiles   []string `json:"removed_files,omitempty"`
	DBRestored     bool     `json:"db_restored"`
	ManifestSHA256 string   `json:"manifest_sha256"`
}

// Rollback verifies snapshot integrity (failing closed on tamper), restores workspace artifacts
// atomically, optionally invokes PostRestoreHook, and polls /_ready === 200 on ReadyURLs.
func Rollback(ctx context.Context, opts RollbackOptions) (*RollbackResult, error) {
	ws := strings.TrimSpace(opts.Workspace)
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" && ws != "" {
		stateDir = filepath.Join(ws, ".grabtaculous")
	}
	rec, err := Verify(stateDir, opts.DeployID)
	if err != nil {
		return nil, err
	}

	targetWS := ws
	if targetWS == "" {
		targetWS = rec.Workspace
	}
	realWS, err := safepath.InitRoot(targetWS)
	if err != nil {
		return nil, err
	}

	snapDir, err := resolveSnapshotDir(stateDir, opts.DeployID)
	if err != nil {
		return nil, err
	}
	payloadDir := filepath.Join(snapDir, "payload")

	expectedSet := make(map[string]struct{}, len(rec.Artifacts))
	for _, art := range rec.Artifacts {
		expectedSet[art.RelPath] = struct{}{}
	}

	// Remove tracked candidate files introduced after the snapshot so restoration is exact.
	var removed []string
	for _, td := range trackedDirs {
		absDir, _, dErr := safepath.ResolveDir(realWS, td.rel)
		if dErr != nil {
			continue
		}
		_ = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, wErr error) error {
			if wErr != nil || d.IsDir() {
				return nil
			}
			rel, rErr := filepath.Rel(realWS, path)
			if rErr != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			if safepath.HasBlockedSegments(relSlash, false) {
				return nil
			}
			if _, ok := expectedSet[relSlash]; !ok {
				if rmErr := os.Remove(path); rmErr == nil {
					removed = append(removed, relSlash)
				}
			}
			return nil
		})
	}
	for _, tf := range trackedTopFiles {
		if _, ok := expectedSet[tf.rel]; !ok {
			if abs, _, rErr := safepath.ResolveFile(realWS, tf.rel, safepath.ResolveOptions{AllowMissing: true, AllowEnvExample: tf.allowEx}); rErr == nil {
				if st, sErr := os.Stat(abs); sErr == nil && st.Mode().IsRegular() {
					if rmErr := os.Remove(abs); rmErr == nil {
						removed = append(removed, tf.rel)
					}
				}
			}
		}
	}

	// Restore every snapshotted file atomically.
	var restored []string
	for _, art := range rec.Artifacts {
		allowEx := strings.EqualFold(filepath.Base(art.RelPath), ".env.example")
		srcAbs, _, err := safepath.ResolveFile(payloadDir, art.RelPath, safepath.ResolveOptions{AllowEnvExample: allowEx})
		if err != nil {
			return nil, err
		}
		data, err := safepath.ReadBoundedFile(srcAbs)
		if err != nil {
			return nil, err
		}
		if err := safepath.WriteFileAtomic(realWS, art.RelPath, data, 0o644, safepath.ResolveOptions{AllowEnvExample: allowEx}); err != nil {
			return nil, err
		}
		restored = append(restored, art.RelPath)
	}

	dbRestored := false
	if rec.DBCheckpoint != nil && rec.DBCheckpoint.Status == "captured" && rec.DBCheckpoint.Driver == "sqlite" && rec.DBCheckpoint.CheckpointFile != "" {
		dbSrc, _, err := safepath.ResolveFile(snapDir, rec.DBCheckpoint.CheckpointFile, safepath.ResolveOptions{})
		if err != nil {
			return nil, err
		}
		dbBytes, err := safepath.ReadBoundedFile(dbSrc)
		if err != nil {
			return nil, err
		}
		baseName := strings.TrimPrefix(filepath.Base(rec.DBCheckpoint.CheckpointFile), "checkpoint-")
		if baseName == "" {
			baseName = "app.db"
		}
		if err := safepath.WriteFileAtomic(realWS, "storage/"+baseName, dbBytes, 0o640, safepath.ResolveOptions{}); err != nil {
			return nil, err
		}
		dbRestored = true
	}

	if opts.PostRestoreHook != nil {
		if err := opts.PostRestoreHook(ctx, rec); err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("post-rollback hook failed: %v", err), "Inspect worker logs after rollback.", err)
		}
	}

	readyVerified := true
	if len(opts.ReadyURLs) > 0 {
		timeout := opts.ReadyTimeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		if err := waitAllReady(ctx, opts.ReadyURLs, timeout); err != nil {
			return nil, err
		}
	}

	return &RollbackResult{
		SchemaVersion:  "1.0.0",
		DeployID:       rec.DeployID,
		Workspace:      realWS,
		Restored:       true,
		ReadyVerified:  readyVerified,
		RestoredFiles:  restored,
		RemovedFiles:   removed,
		DBRestored:     dbRestored,
		ManifestSHA256: rec.ManifestSHA256,
	}, nil
}

func waitAllReady(ctx context.Context, urls []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for _, rawURL := range urls {
		ready := false
		for {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err == nil {
				resp, doErr := client.Do(req)
				if doErr == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						ready = true
						break
					}
				}
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				break
			}
			select {
			case <-ctx.Done():
				return safepath.WrapError(safepath.ErrSnapshotFailed, "rollback readiness check cancelled", "Retry rollback status check.", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		if !ready {
			return safepath.NewError(
				safepath.ErrSnapshotFailed,
				fmt.Sprintf("post-rollback readiness check failed: %s did not return HTTP 200 within %s", rawURL, timeout),
				"Inspect restored worker logs and verify /_ready.",
			)
		}
	}
	return nil
}
