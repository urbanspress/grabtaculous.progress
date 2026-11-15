package snapshot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grabtaculous/internal/safepath"
)

// Artifact records a single immutable file captured in a Pre-Deploy Snapshot.
type Artifact struct {
	RelPath   string `json:"rel_path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Category  string `json:"category"`
}

// DBCheckpoint records pre-deploy database backup hook metadata.
type DBCheckpoint struct {
	Driver         string `json:"driver,omitempty"`
	Hook           string `json:"hook,omitempty"`
	Status         string `json:"status"`
	CheckpointFile string `json:"checkpoint_file,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
}

// Record represents an immutable Pre-Deploy Snapshot manifest (`snapshot.json`).
type Record struct {
	SchemaVersion  string        `json:"schema_version"`
	DeployID       string        `json:"deploy_id"`
	AppName        string        `json:"app_name"`
	Workspace      string        `json:"workspace"`
	CreatedAtUTC   string        `json:"created_at_utc"`
	Trigger        string        `json:"trigger"`
	Artifacts      []Artifact    `json:"artifacts"`
	DBCheckpoint   *DBCheckpoint `json:"db_checkpoint,omitempty"`
	ManifestSHA256 string        `json:"manifest_sha256"`
}

// Summary provides a compact listing view of a snapshot.
type Summary struct {
	DeployID       string `json:"deploy_id"`
	AppName        string `json:"app_name"`
	CreatedAtUTC   string `json:"created_at_utc"`
	Trigger        string `json:"trigger"`
	ArtifactCount  int    `json:"artifact_count"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

// CreateOptions configures Pre-Deploy Snapshot capture.
type CreateOptions struct {
	StateDir         string
	Workspace        string
	AppName          string
	Trigger          string
	DeployID         string
	Now              func() time.Time
	DBCheckpointHook func(ctx context.Context, workspace string, dbOutDir string) (*DBCheckpoint, error)
}

type manifestCore struct {
	SchemaVersion string        `json:"schema_version"`
	DeployID      string        `json:"deploy_id"`
	AppName       string        `json:"app_name"`
	Workspace     string        `json:"workspace"`
	CreatedAtUTC  string        `json:"created_at_utc"`
	Trigger       string        `json:"trigger"`
	Artifacts     []Artifact    `json:"artifacts"`
	DBCheckpoint  *DBCheckpoint `json:"db_checkpoint,omitempty"`
}

var trackedTopFiles = []struct {
	rel      string
	category string
	allowEx  bool
}{
	{"grabman.json", "manifest", false},
	{"grabman.lock", "lockfile", false},
	{"grab.platform.json", "config", false},
	{"grab.data.json", "config", false},
	{"grab.api.json", "config", false},
	{"grab.connect.lock", "lockfile", false},
	{"deploy/docker-compose.yml", "compose", false},
	{"deploy/Caddyfile", "edge", false},
	{"deploy/nginx.conf", "edge", false},
	{"deploy/cloudflared.yml", "edge", false},
	{"deploy/.env.example", "config", true},
	{"storage/site-dna.json", "site_dna", false},
	{"storage/SITE-CONTEXT.md", "site_dna", false},
	{"storage/GRAB-DESIGN-SYSTEM.md", "design_system", false},
}

var trackedDirs = []struct {
	rel      string
	category string
}{
	{"public/grex", "static"},
	{"public/assets", "static"},
	{"theme", "theme"},
	{"src", "source"},
	{"views", "views"},
}

// Create captures an immutable, SHA-256-verified Pre-Deploy Snapshot of workspace under stateDir/snapshots/<deploy_id>.
func Create(ctx context.Context, opts CreateOptions) (*Record, error) {
	realWS, err := safepath.ResolveRoot(opts.Workspace)
	if err != nil {
		return nil, err
	}
	stateRoot, err := safepath.InitRoot(resolveStateDir(opts.StateDir, realWS))
	if err != nil {
		return nil, err
	}

	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	nowUTC := nowFn().UTC()

	deployID := strings.TrimSpace(opts.DeployID)
	if deployID == "" {
		deployID = generateDeployID(nowUTC)
	}
	if safepath.HasBlockedSegments(deployID, false) || strings.ContainsAny(deployID, `/\ `) {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("invalid deploy_id %q", deployID), "Use alphanumeric deploy IDs with hyphens/underscores.")
	}

	appName := strings.TrimSpace(opts.AppName)
	if appName == "" {
		appName = filepath.Base(realWS)
	}
	trigger := strings.TrimSpace(opts.Trigger)
	if trigger == "" {
		trigger = "manual"
	}

	snapDir := filepath.Join(stateRoot, "snapshots", deployID)
	if _, statErr := os.Stat(snapDir); statErr == nil {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q already exists (immutable snapshots cannot be overwritten)", deployID), "Provide a new deploy_id.")
	}
	payloadDir := filepath.Join(snapDir, "payload")
	if err := os.MkdirAll(payloadDir, 0o750); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "failed to create snapshot directory", "Check state directory permissions.", err)
	}

	seen := map[string]struct{}{}
	var artifacts []Artifact

	captureOne := func(relSlash, category string, allowEx bool) error {
		if _, dup := seen[relSlash]; dup {
			return nil
		}
		abs, cleanRel, rErr := safepath.ResolveFile(realWS, relSlash, safepath.ResolveOptions{AllowMissing: true, AllowEnvExample: allowEx})
		if rErr != nil {
			return rErr
		}
		st, statErr := os.Stat(abs)
		if statErr != nil || !st.Mode().IsRegular() {
			return nil
		}
		data, readErr := safepath.ReadBoundedFile(abs)
		if readErr != nil {
			return readErr
		}
		if err := safepath.WriteFileAtomic(payloadDir, cleanRel, data, 0o644, safepath.ResolveOptions{AllowEnvExample: allowEx}); err != nil {
			return err
		}
		seen[cleanRel] = struct{}{}
		artifacts = append(artifacts, Artifact{
			RelPath:   cleanRel,
			SHA256:    safepath.SHA256Bytes(data),
			SizeBytes: int64(len(data)),
			Category:  category,
		})
		return nil
	}

	for _, tf := range trackedTopFiles {
		if err := captureOne(tf.rel, tf.category, tf.allowEx); err != nil {
			_ = os.RemoveAll(snapDir)
			return nil, err
		}
	}

	for _, td := range trackedDirs {
		absDir, _, dErr := safepath.ResolveDir(realWS, td.rel)
		if dErr != nil {
			if os.IsNotExist(dErr) {
				continue
			}
			_ = os.RemoveAll(snapDir)
			return nil, dErr
		}
		walkErr := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			if d.IsDir() {
				if safepath.IsSensitiveSegment(d.Name(), false) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, rErr := filepath.Rel(realWS, path)
			if rErr != nil {
				return rErr
			}
			relSlash := filepath.ToSlash(rel)
			if safepath.HasBlockedSegments(relSlash, false) {
				return nil
			}
			return captureOne(relSlash, td.category, false)
		})
		if walkErr != nil {
			_ = os.RemoveAll(snapDir)
			return nil, walkErr
		}
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].RelPath < artifacts[j].RelPath
	})

	dbOutDir := filepath.Join(snapDir, "db")
	dbCP, cpErr := captureDBCheckpoint(ctx, realWS, dbOutDir, opts.DBCheckpointHook)
	if cpErr != nil {
		_ = os.RemoveAll(snapDir)
		return nil, cpErr
	}

	rec := &Record{
		SchemaVersion: "1.0.0",
		DeployID:      deployID,
		AppName:       appName,
		Workspace:     realWS,
		CreatedAtUTC:  nowUTC.Format(time.RFC3339),
		Trigger:       trigger,
		Artifacts:     artifacts,
		DBCheckpoint:  dbCP,
	}
	digest, err := computeRecordDigest(rec)
	if err != nil {
		_ = os.RemoveAll(snapDir)
		return nil, err
	}
	rec.ManifestSHA256 = digest

	rawMeta, err := safepath.CanonicalJSON(rec)
	if err != nil {
		_ = os.RemoveAll(snapDir)
		return nil, err
	}
	if err := safepath.WriteFileAtomic(snapDir, "snapshot.json", rawMeta, 0o644, safepath.ResolveOptions{}); err != nil {
		_ = os.RemoveAll(snapDir)
		return nil, err
	}

	return rec, nil
}

func captureDBCheckpoint(ctx context.Context, realWS, dbOutDir string, hook func(context.Context, string, string) (*DBCheckpoint, error)) (*DBCheckpoint, error) {
	if hook != nil {
		if err := os.MkdirAll(dbOutDir, 0o750); err != nil {
			return nil, err
		}
		cp, err := hook(ctx, realWS, dbOutDir)
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("pre-deploy DB checkpoint hook failed: %v", err), "Verify database backup hook health before deploying.", err)
		}
		return cp, nil
	}

	// Best-effort local SQLite checkpoint if storage/*.db exists inside workspace.
	storageDir, _, err := safepath.ResolveDir(realWS, "storage")
	if err != nil {
		return &DBCheckpoint{Status: "none"}, nil
	}
	entries, readErr := os.ReadDir(storageDir)
	if readErr != nil {
		return &DBCheckpoint{Status: "none"}, nil
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".db") {
			continue
		}
		rel := "storage/" + ent.Name()
		abs, _, rErr := safepath.ResolveFile(realWS, rel, safepath.ResolveOptions{})
		if rErr != nil {
			return nil, rErr
		}
		rawDB, readDBErr := safepath.ReadBoundedFile(abs)
		if readDBErr != nil {
			return nil, readDBErr
		}
		if err := os.MkdirAll(dbOutDir, 0o750); err != nil {
			return nil, err
		}
		cpRel := "checkpoint-" + ent.Name()
		if err := safepath.WriteFileAtomic(dbOutDir, cpRel, rawDB, 0o644, safepath.ResolveOptions{}); err != nil {
			return nil, err
		}
		return &DBCheckpoint{
			Driver:         "sqlite",
			Hook:           "sqlite-copy",
			Status:         "captured",
			CheckpointFile: "db/" + cpRel,
			SHA256:         safepath.SHA256Bytes(rawDB),
			SizeBytes:      int64(len(rawDB)),
		}, nil
	}
	return &DBCheckpoint{Status: "none"}, nil
}

// Verify loads snapshot.json and cryptographically validates ManifestSHA256 and every payload/DB artifact.
func Verify(stateDir, deployID string) (*Record, error) {
	snapDir, err := resolveSnapshotDir(stateDir, deployID)
	if err != nil {
		return nil, err
	}
	metaAbs, _, err := safepath.ResolveFile(snapDir, "snapshot.json", safepath.ResolveOptions{})
	if err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q metadata missing", deployID), "List valid snapshots with 'grabtaculous snapshot list'.", err)
	}
	rawMeta, err := safepath.ReadBoundedFile(metaAbs)
	if err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "failed to read snapshot.json", "Verify snapshot permissions.", err)
	}
	var rec Record
	dec := json.NewDecoder(bytes.NewReader(rawMeta))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("malformed snapshot.json for %q: %v", deployID, err), "Snapshot metadata is corrupted.", err)
	}
	if rec.DeployID != deployID {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot deploy_id mismatch (%q vs %q)", rec.DeployID, deployID), "Do not rename snapshot directories manually.")
	}
	computedDigest, err := computeRecordDigest(&rec)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(computedDigest, rec.ManifestSHA256) {
		return nil, safepath.NewError(
			safepath.ErrSnapshotFailed,
			fmt.Sprintf("snapshot %q manifest tamper/corruption detected (stored %s != computed %s)", deployID, rec.ManifestSHA256, computedDigest),
			"Refusing rollback from tampered snapshot; select another verified deploy_id.",
		)
	}

	payloadDir := filepath.Join(snapDir, "payload")
	for _, art := range rec.Artifacts {
		allowEx := strings.EqualFold(filepath.Base(art.RelPath), ".env.example")
		abs, _, fErr := safepath.ResolveFile(payloadDir, art.RelPath, safepath.ResolveOptions{AllowEnvExample: allowEx})
		if fErr != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q missing artifact %q", deployID, art.RelPath), "Refusing rollback from incomplete snapshot.", fErr)
		}
		data, rErr := safepath.ReadBoundedFile(abs)
		if rErr != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q unreadable artifact %q", deployID, art.RelPath), "Check snapshot permissions.", rErr)
		}
		if int64(len(data)) != art.SizeBytes {
			return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q size mismatch on %q (expected %d, got %d)", deployID, art.RelPath, art.SizeBytes, len(data)), "Refusing rollback from tampered snapshot.")
		}
		actualHash := safepath.SHA256Bytes(data)
		if !strings.EqualFold(actualHash, art.SHA256) {
			return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q SHA-256 tamper detected on %q (expected %s, got %s)", deployID, art.RelPath, art.SHA256, actualHash), "Refusing rollback from tampered snapshot.")
		}
	}

	if rec.DBCheckpoint != nil && rec.DBCheckpoint.Status == "captured" && rec.DBCheckpoint.CheckpointFile != "" {
		dbAbs, _, fErr := safepath.ResolveFile(snapDir, rec.DBCheckpoint.CheckpointFile, safepath.ResolveOptions{})
		if fErr != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q missing DB checkpoint %q", deployID, rec.DBCheckpoint.CheckpointFile), "Refusing rollback without verified DB checkpoint.", fErr)
		}
		dbHash, hErr := safepath.SHA256File(dbAbs)
		if hErr != nil {
			return nil, hErr
		}
		if !strings.EqualFold(dbHash, rec.DBCheckpoint.SHA256) {
			return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q DB checkpoint SHA-256 tamper detected (%s != %s)", deployID, dbHash, rec.DBCheckpoint.SHA256), "Refusing rollback from tampered DB snapshot.")
		}
	}

	return &rec, nil
}

// List enumerates snapshots in stateDir sorted newest-first.
func List(stateDir string) ([]Summary, error) {
	root, err := safepath.ResolveRoot(stateDir)
	if err != nil {
		return nil, err
	}
	snapsRoot := filepath.Join(root, "snapshots")
	entries, err := os.ReadDir(snapsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []Summary{}, nil
		}
		return nil, err
	}
	var out []Summary
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		metaPath := filepath.Join(snapsRoot, ent.Name(), "snapshot.json")
		raw, rErr := safepath.ReadBoundedFile(metaPath)
		if rErr != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(raw, &rec) == nil {
			out = append(out, Summary{
				DeployID:       rec.DeployID,
				AppName:        rec.AppName,
				CreatedAtUTC:   rec.CreatedAtUTC,
				Trigger:        rec.Trigger,
				ArtifactCount:  len(rec.Artifacts),
				ManifestSHA256: rec.ManifestSHA256,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAtUTC != out[j].CreatedAtUTC {
			return out[i].CreatedAtUTC > out[j].CreatedAtUTC
		}
		return out[i].DeployID > out[j].DeployID
	})
	return out, nil
}

func resolveSnapshotDir(stateDir, deployID string) (string, error) {
	id := strings.TrimSpace(deployID)
	if id == "" || safepath.HasBlockedSegments(id, false) || strings.ContainsAny(id, `/\ `) {
		return "", safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("invalid deploy_id %q", deployID), "Pass a valid snapshot deploy_id.")
	}
	stateRoot, err := safepath.ResolveRoot(stateDir)
	if err != nil {
		return "", err
	}
	snapDir, _, err := safepath.ResolveDir(stateRoot, filepath.Join("snapshots", id))
	if err != nil {
		return "", safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("snapshot %q not found", id), "Check available snapshots with 'grabtaculous snapshot list'.", err)
	}
	return snapDir, nil
}

func resolveStateDir(stateDir, workspace string) string {
	if strings.TrimSpace(stateDir) != "" {
		return strings.TrimSpace(stateDir)
	}
	return filepath.Join(workspace, ".grabtaculous")
}

func computeRecordDigest(rec *Record) (string, error) {
	core := manifestCore{
		SchemaVersion: rec.SchemaVersion,
		DeployID:      rec.DeployID,
		AppName:       rec.AppName,
		Workspace:     rec.Workspace,
		CreatedAtUTC:  rec.CreatedAtUTC,
		Trigger:       rec.Trigger,
		Artifacts:     rec.Artifacts,
		DBCheckpoint:  rec.DBCheckpoint,
	}
	raw, err := safepath.CanonicalJSON(core)
	if err != nil {
		return "", err
	}
	return safepath.SHA256Bytes(raw), nil
}

func generateDeployID(nowUTC time.Time) string {
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	return fmt.Sprintf("dep-%s-%s", nowUTC.Format("20060102T150405Z"), hex.EncodeToString(rnd[:]))
}
