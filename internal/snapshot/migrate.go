package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grabtaculous/internal/compose"
	"grabtaculous/internal/safepath"
)

// MigrationManifest records all SHA-256 verified artifacts inside a Contabo -> Bluehost migration bundle.
type MigrationManifest struct {
	SchemaVersion  string     `json:"schema_version"`
	BundleID       string     `json:"bundle_id"`
	SourceProfile  string     `json:"source_profile"`
	TargetProfile  string     `json:"target_profile"`
	CreatedAtUTC   string     `json:"created_at_utc"`
	TotalBytes     int64      `json:"total_bytes"`
	HasPostgres    bool       `json:"has_postgres"`
	HasMariaDB     bool       `json:"has_mariadb"`
	Artifacts      []Artifact `json:"artifacts"`
	ManifestSHA256 string     `json:"manifest_sha256"`
}

type migrationManifestCore struct {
	SchemaVersion string     `json:"schema_version"`
	BundleID      string     `json:"bundle_id"`
	SourceProfile string     `json:"source_profile"`
	TargetProfile string     `json:"target_profile"`
	CreatedAtUTC  string     `json:"created_at_utc"`
	TotalBytes    int64      `json:"total_bytes"`
	HasPostgres   bool       `json:"has_postgres"`
	HasMariaDB    bool       `json:"has_mariadb"`
	Artifacts     []Artifact `json:"artifacts"`
}

// MigrateExportOptions configures deterministic node-to-node migration bundle export.
type MigrateExportOptions struct {
	SourceWorkspace string
	OutputBundle    string
	SourceProfile   string
	TargetProfile   string
	UploadsDir      string
	GrexPublicDir   string
	PostgresDump    string
	MariaDBDump     string
	BundleID        string
	Now             func() time.Time
	DiskStatFn      compose.DiskStatFunc
}

// MigrateImportOptions configures SHA-256 verified import onto the target VPS (e.g. Bluehost vps-8c-16g).
type MigrateImportOptions struct {
	BundlePath      string
	TargetWorkspace string
	TargetProfile   string
	UploadsDir      string
	GrexPublicDir   string
	VerifyOnly      bool
	DiskStatFn      compose.DiskStatFunc
}

// MigrateImportResult summarizes a verified or applied node migration import.
type MigrateImportResult struct {
	SchemaVersion    string   `json:"schema_version"`
	BundleID         string   `json:"bundle_id"`
	SourceProfile    string   `json:"source_profile"`
	TargetProfile    string   `json:"target_profile"`
	Verified         bool     `json:"verified"`
	Applied          bool     `json:"applied"`
	ArtifactCount    int      `json:"artifact_count"`
	TotalBytes       int64    `json:"total_bytes"`
	RestoredFiles    []string `json:"restored_files"`
	PostgresDumpPath string   `json:"postgres_dump_path,omitempty"`
	MariaDBDumpPath  string   `json:"mariadb_dump_path,omitempty"`
	ReadyForCutover  bool     `json:"ready_for_cutover"`
}

// MigrateExport exports SHA-256 verified DB dumps, uploads, compiled /static/grex/, Site DNA, and grabman.lock.
func MigrateExport(_ context.Context, opts MigrateExportOptions) (*MigrationManifest, error) {
	srcWS, err := safepath.ResolveRoot(opts.SourceWorkspace)
	if err != nil {
		return nil, err
	}
	outPath := strings.TrimSpace(opts.OutputBundle)
	if outPath == "" {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, "output bundle path is required", "Pass --out <dir-or-.tar.gz>.")
	}
	srcProf := strings.TrimSpace(opts.SourceProfile)
	if srcProf == "" {
		srcProf = "vps-6c-12g"
	}
	dstProf := strings.TrimSpace(opts.TargetProfile)
	if dstProf == "" {
		dstProf = "vps-8c-16g"
	}
	if _, err := compose.GetProfile(srcProf); err != nil {
		return nil, err
	}
	if _, err := compose.GetProfile(dstProf); err != nil {
		return nil, err
	}

	if _, err := compose.CheckDiskReserve(filepath.Dir(outPath), 0, opts.DiskStatFn); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, err.Error(), "Keep >= 15 GB NVMe free before exporting migration bundle.", err)
	}

	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	nowUTC := nowFn().UTC()
	bundleID := strings.TrimSpace(opts.BundleID)
	if bundleID == "" {
		bundleID = "mig-" + nowUTC.Format("20060102T150405Z")
	}

	type bundleFile struct {
		rel      string
		category string
		data     []byte
		allowEx  bool
	}
	files := map[string]bundleFile{}

	addFile := func(relSlash, category string, data []byte, allowEx bool) error {
		cleanRel := filepath.ToSlash(filepath.Clean(relSlash))
		if safepath.HasBlockedSegments(cleanRel, allowEx) {
			return nil
		}
		if _, dup := files[cleanRel]; dup {
			return nil
		}
		files[cleanRel] = bundleFile{
			rel:      cleanRel,
			category: category,
			data:     data,
			allowEx:  allowEx,
		}
		return nil
	}

	// 1. Tracked workspace config, lockfiles, Site DNA, source, views, themes
	for _, tf := range trackedTopFiles {
		abs, cleanRel, rErr := safepath.ResolveFile(srcWS, tf.rel, safepath.ResolveOptions{AllowMissing: true, AllowEnvExample: tf.allowEx})
		if rErr != nil {
			return nil, rErr
		}
		st, sErr := os.Stat(abs)
		if sErr != nil || !st.Mode().IsRegular() {
			continue
		}
		data, readErr := safepath.ReadBoundedFile(abs)
		if readErr != nil {
			return nil, readErr
		}
		if err := addFile(cleanRel, tf.category, data, tf.allowEx); err != nil {
			return nil, err
		}
	}

	collectDir := func(baseRoot, prefixRel, category string) error {
		realDir, err := safepath.ResolveRoot(baseRoot)
		if err != nil {
			return err
		}
		return filepath.WalkDir(realDir, func(p string, d fs.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			if d.IsDir() {
				if safepath.IsSensitiveSegment(d.Name(), false) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, rErr := filepath.Rel(realDir, p)
			if rErr != nil {
				return rErr
			}
			relSlash := filepath.ToSlash(filepath.Join(prefixRel, rel))
			if safepath.HasBlockedSegments(relSlash, false) {
				return nil
			}
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			return addFile(relSlash, category, data, false)
		})
	}

	for _, td := range trackedDirs {
		absDir := filepath.Join(srcWS, filepath.FromSlash(td.rel))
		if st, err := os.Stat(absDir); err == nil && st.IsDir() {
			if err := collectDir(absDir, td.rel, td.category); err != nil {
				return nil, err
			}
		}
	}

	// 2. Uploads directory (override or workspace/uploads)
	uploadsDir := strings.TrimSpace(opts.UploadsDir)
	if uploadsDir == "" {
		uploadsDir = filepath.Join(srcWS, "uploads")
	}
	if st, err := os.Stat(uploadsDir); err == nil && st.IsDir() {
		if err := collectDir(uploadsDir, "uploads", "media"); err != nil {
			return nil, err
		}
	}

	// 3. Grex public directory override if external (/var/lib/grisp/public/grex)
	if strings.TrimSpace(opts.GrexPublicDir) != "" {
		if st, err := os.Stat(opts.GrexPublicDir); err == nil && st.IsDir() {
			if err := collectDir(opts.GrexPublicDir, "public/grex", "static"); err != nil {
				return nil, err
			}
		}
	}

	// 4. PostgreSQL 18 & MariaDB 11.8 database dumps
	hasPG := false
	if strings.TrimSpace(opts.PostgresDump) != "" {
		absPG, err := safepath.ResolveExternalReadFile(opts.PostgresDump)
		if err != nil {
			return nil, err
		}
		pgBytes, err := os.ReadFile(absPG)
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "failed reading PostgreSQL dump", "Verify --pg-dump path.", err)
		}
		if err := addFile("db/postgres-18.sql", "database", pgBytes, false); err != nil {
			return nil, err
		}
		hasPG = true
	}
	hasMaria := false
	if strings.TrimSpace(opts.MariaDBDump) != "" {
		absMDB, err := safepath.ResolveExternalReadFile(opts.MariaDBDump)
		if err != nil {
			return nil, err
		}
		mdbBytes, err := os.ReadFile(absMDB)
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "failed reading MariaDB dump", "Verify --mariadb-dump path.", err)
		}
		if err := addFile("db/mariadb-11-8.sql", "database", mdbBytes, false); err != nil {
			return nil, err
		}
		hasMaria = true
	}

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var totalBytes int64
	artifacts := make([]Artifact, 0, len(keys))
	for _, k := range keys {
		bf := files[k]
		sz := int64(len(bf.data))
		totalBytes += sz
		artifacts = append(artifacts, Artifact{
			RelPath:   bf.rel,
			SHA256:    safepath.SHA256Bytes(bf.data),
			SizeBytes: sz,
			Category:  bf.category,
		})
	}

	if _, err := compose.CheckDiskReserve(filepath.Dir(outPath), totalBytes, opts.DiskStatFn); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, err.Error(), "Insufficient NVMe reserve for migration bundle export.", err)
	}

	manifest := &MigrationManifest{
		SchemaVersion: "1.0.0",
		BundleID:      bundleID,
		SourceProfile: srcProf,
		TargetProfile: dstProf,
		CreatedAtUTC:  nowUTC.Format(time.RFC3339),
		TotalBytes:    totalBytes,
		HasPostgres:   hasPG,
		HasMariaDB:    hasMaria,
		Artifacts:     artifacts,
	}
	digest, err := computeMigrationDigest(manifest)
	if err != nil {
		return nil, err
	}
	manifest.ManifestSHA256 = digest

	rawManifest, err := safepath.CanonicalJSON(manifest)
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(strings.ToLower(outPath), ".tar.gz") {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		writeTarEntry := func(name string, content []byte, mode int64) error {
			hdr := &tar.Header{
				Name:    name,
				Mode:    mode,
				Size:    int64(len(content)),
				ModTime: nowUTC,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			_, err := tw.Write(content)
			return err
		}
		if err := writeTarEntry("migration-manifest.json", rawManifest, 0o644); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			return nil, err
		}
		for _, k := range keys {
			bf := files[k]
			if err := writeTarEntry("payload/"+bf.rel, bf.data, 0o644); err != nil {
				_ = tw.Close()
				_ = gz.Close()
				_ = f.Close()
				return nil, err
			}
		}
		if err := tw.Close(); err != nil {
			_ = gz.Close()
			_ = f.Close()
			return nil, err
		}
		if err := gz.Close(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return manifest, nil
	}

	bundleRoot, err := safepath.InitRoot(outPath)
	if err != nil {
		return nil, err
	}
	payloadRoot := filepath.Join(bundleRoot, "payload")
	if err := os.MkdirAll(payloadRoot, 0o750); err != nil {
		return nil, err
	}
	for _, k := range keys {
		bf := files[k]
		if err := safepath.WriteFileAtomic(payloadRoot, bf.rel, bf.data, 0o644, safepath.ResolveOptions{AllowEnvExample: bf.allowEx}); err != nil {
			return nil, err
		}
	}
	if err := safepath.WriteFileAtomic(bundleRoot, "migration-manifest.json", rawManifest, 0o644, safepath.ResolveOptions{}); err != nil {
		return nil, err
	}
	return manifest, nil
}

// MigrateImport verifies every SHA-256 digest and the 15 GB NVMe reserve before restoring onto TargetWorkspace.
func MigrateImport(_ context.Context, opts MigrateImportOptions) (*MigrateImportResult, error) {
	bundlePath := strings.TrimSpace(opts.BundlePath)
	if bundlePath == "" {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, "bundle path is required", "Pass --bundle <dir-or-.tar.gz>.")
	}
	targetWS, err := safepath.InitRoot(opts.TargetWorkspace)
	if err != nil {
		return nil, err
	}

	payloadMap := map[string][]byte{}
	var rawManifest []byte

	if strings.HasSuffix(strings.ToLower(bundlePath), ".tar.gz") {
		f, err := os.Open(bundlePath)
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "failed opening migration tar.gz", "Verify --bundle path.", err)
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "corrupt gzip migration bundle", "Re-export migration bundle.", err)
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "corrupt tar migration bundle", "Re-export migration bundle.", err)
			}
			if hdr.Typeflag != tar.TypeReg {
				continue
			}
			cleanName := filepath.ToSlash(filepath.Clean(hdr.Name))
			if strings.HasPrefix(cleanName, "/") || strings.HasPrefix(cleanName, "../") || strings.Contains(cleanName, "/../") {
				return nil, safepath.NewError(safepath.ErrPathConfinement, fmt.Sprintf("unsafe tar entry %q", hdr.Name), "Rejecting path traversal in migration archive.")
			}
			data, err := io.ReadAll(io.LimitReader(tr, 256*1024*1024))
			if err != nil {
				return nil, err
			}
			if cleanName == "migration-manifest.json" {
				rawManifest = data
			} else if strings.HasPrefix(cleanName, "payload/") {
				rel := strings.TrimPrefix(cleanName, "payload/")
				allowEx := strings.HasSuffix(rel, ".env.example")
				if safepath.HasBlockedSegments(rel, allowEx) {
					return nil, safepath.NewError(safepath.ErrPathConfinement, fmt.Sprintf("sensitive path %q rejected inside migration bundle", rel), "Never bundle .env or key files.")
				}
				payloadMap[rel] = data
			}
		}
	} else {
		bundleRoot, err := safepath.ResolveRoot(bundlePath)
		if err != nil {
			return nil, err
		}
		manAbs, _, err := safepath.ResolveFile(bundleRoot, "migration-manifest.json", safepath.ResolveOptions{})
		if err != nil {
			return nil, safepath.WrapError(safepath.ErrSnapshotFailed, "missing migration-manifest.json", "Verify bundle directory.", err)
		}
		rawManifest, err = safepath.ReadBoundedFile(manAbs)
		if err != nil {
			return nil, err
		}
		payloadRoot := filepath.Join(bundleRoot, "payload")
		if err := filepath.WalkDir(payloadRoot, func(p string, d fs.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			if d.IsDir() {
				return nil
			}
			rel, rErr := filepath.Rel(payloadRoot, p)
			if rErr != nil {
				return rErr
			}
			relSlash := filepath.ToSlash(rel)
			allowEx := strings.HasSuffix(relSlash, ".env.example")
			if safepath.HasBlockedSegments(relSlash, allowEx) {
				return safepath.NewError(safepath.ErrPathConfinement, fmt.Sprintf("sensitive path %q rejected inside migration bundle", relSlash), "Never bundle .env or key files.")
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			payloadMap[relSlash] = data
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if len(rawManifest) == 0 {
		return nil, safepath.NewError(safepath.ErrSnapshotFailed, "migration-manifest.json is missing or empty", "Re-export migration bundle.")
	}
	var manifest MigrationManifest
	dec := json.NewDecoder(bytes.NewReader(rawManifest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, fmt.Sprintf("malformed migration-manifest.json: %v", err), "Re-export migration bundle.", err)
	}
	expectedDigest, err := computeMigrationDigest(&manifest)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(expectedDigest, strings.TrimSpace(manifest.ManifestSHA256)) {
		return nil, safepath.NewError(
			safepath.ErrSnapshotFailed,
			fmt.Sprintf("migration manifest SHA-256 tamper detected (expected %s, got %s)", expectedDigest, manifest.ManifestSHA256),
			"Refusing cutover import; re-export verified migration bundle from source node.",
		)
	}

	// Verify every artifact SHA-256 digest & size before writing a single byte!
	var computedTotal int64
	for _, art := range manifest.Artifacts {
		allowEx := strings.HasSuffix(art.RelPath, ".env.example")
		if safepath.HasBlockedSegments(art.RelPath, allowEx) {
			return nil, safepath.NewError(safepath.ErrPathConfinement, fmt.Sprintf("blocked path %q in manifest", art.RelPath), "Aborting import.")
		}
		data, ok := payloadMap[art.RelPath]
		if !ok {
			return nil, safepath.NewError(safepath.ErrSnapshotFailed, fmt.Sprintf("migration bundle missing artifact %q", art.RelPath), "Re-export complete migration bundle.")
		}
		actualSHA := safepath.SHA256Bytes(data)
		if !strings.EqualFold(actualSHA, art.SHA256) || int64(len(data)) != art.SizeBytes {
			return nil, safepath.NewError(
				safepath.ErrSnapshotFailed,
				fmt.Sprintf("migration artifact %q failed SHA-256 verification (tamper/truncation detected)", art.RelPath),
				"Aborting cutover import; re-transfer bundle over SSH/SCP.",
			)
		}
		computedTotal += int64(len(data))
	}

	// Enforce 15 GB NVMe reserve guard accounting for payload size
	if _, err := compose.CheckDiskReserve(targetWS, computedTotal, opts.DiskStatFn); err != nil {
		return nil, safepath.WrapError(safepath.ErrSnapshotFailed, err.Error(), "Target VPS must retain >= 15 GB free NVMe space after migration import.", err)
	}

	targetProf := strings.TrimSpace(opts.TargetProfile)
	if targetProf == "" {
		targetProf = manifest.TargetProfile
	}
	if targetProf == "" {
		targetProf = "vps-8c-16g"
	}
	if _, err := compose.GetProfile(targetProf); err != nil {
		return nil, err
	}

	res := &MigrateImportResult{
		SchemaVersion: "1.0.0",
		BundleID:      manifest.BundleID,
		SourceProfile: manifest.SourceProfile,
		TargetProfile: targetProf,
		Verified:      true,
		Applied:       !opts.VerifyOnly,
		ArtifactCount: len(manifest.Artifacts),
		TotalBytes:    computedTotal,
	}

	if opts.VerifyOnly {
		res.ReadyForCutover = true
		return res, nil
	}

	for _, art := range manifest.Artifacts {
		data := payloadMap[art.RelPath]
		destRoot := targetWS
		destRel := art.RelPath

		if strings.HasPrefix(art.RelPath, "uploads/") && strings.TrimSpace(opts.UploadsDir) != "" {
			upRoot, upErr := safepath.InitRoot(opts.UploadsDir)
			if upErr != nil {
				return nil, upErr
			}
			destRoot = upRoot
			destRel = strings.TrimPrefix(art.RelPath, "uploads/")
		} else if strings.HasPrefix(art.RelPath, "public/grex/") && strings.TrimSpace(opts.GrexPublicDir) != "" {
			grexRoot, gErr := safepath.InitRoot(opts.GrexPublicDir)
			if gErr != nil {
				return nil, gErr
			}
			destRoot = grexRoot
			destRel = strings.TrimPrefix(art.RelPath, "public/grex/")
		}

		allowEx := strings.HasSuffix(art.RelPath, ".env.example")
		if err := safepath.WriteFileAtomic(destRoot, destRel, data, 0o644, safepath.ResolveOptions{AllowEnvExample: allowEx}); err != nil {
			return nil, err
		}
		res.RestoredFiles = append(res.RestoredFiles, art.RelPath)
		if art.RelPath == "db/postgres-18.sql" {
			res.PostgresDumpPath = filepath.Join(destRoot, filepath.FromSlash(destRel))
		}
		if art.RelPath == "db/mariadb-11-8.sql" {
			res.MariaDBDumpPath = filepath.Join(destRoot, filepath.FromSlash(destRel))
		}
	}

	sort.Strings(res.RestoredFiles)
	res.ReadyForCutover = true
	return res, nil
}

func computeMigrationDigest(m *MigrationManifest) (string, error) {
	core := migrationManifestCore{
		SchemaVersion: m.SchemaVersion,
		BundleID:      m.BundleID,
		SourceProfile: m.SourceProfile,
		TargetProfile: m.TargetProfile,
		CreatedAtUTC:  m.CreatedAtUTC,
		TotalBytes:    m.TotalBytes,
		HasPostgres:   m.HasPostgres,
		HasMariaDB:    m.HasMariaDB,
		Artifacts:     m.Artifacts,
	}
	raw, err := safepath.CanonicalJSON(core)
	if err != nil {
		return "", err
	}
	return safepath.SHA256Bytes(raw), nil
}
