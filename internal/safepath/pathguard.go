package safepath

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MaxReadBytes caps workspace file reads at 10 MiB matching GRAB-E401 confinement.
const MaxReadBytes = 10 * 1024 * 1024

// ResolveOptions tunes path confinement for explicitly safe template outputs such as .env.example.
type ResolveOptions struct {
	AllowMissing    bool
	AllowEnvExample bool
}

// IsSensitiveSegment reports whether a path segment targets secrets, private keys, or VCS metadata.
func IsSensitiveSegment(seg string, allowEnvExample bool) bool {
	lower := strings.ToLower(strings.TrimSpace(seg))
	if lower == "" {
		return false
	}
	switch lower {
	case ".git", ".env", ".ssh", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa":
		return true
	}
	if strings.HasPrefix(lower, ".env.") {
		if allowEnvExample && lower == ".env.example" {
			return false
		}
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") ||
		strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
		return true
	}
	return false
}

// HasBlockedSegments detects empty paths, null bytes, parent traversal (..), and sensitive segments.
func HasBlockedSegments(rawPath string, allowEnvExample bool) bool {
	if rawPath == "" || strings.ContainsRune(rawPath, '\x00') {
		return true
	}
	segments := strings.FieldsFunc(rawPath, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(segments) == 0 {
		return true
	}
	for _, seg := range segments {
		if seg == ".." || IsSensitiveSegment(seg, allowEnvExample) {
			return true
		}
	}
	return false
}

// EvalExistingSymlinks resolves symlinks for existing paths or the deepest existing ancestor.
func EvalExistingSymlinks(absPath string) (string, error) {
	real, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absPath)
	if parent == absPath {
		return absPath, nil
	}
	realParent, err := EvalExistingSymlinks(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(realParent, filepath.Base(absPath)), nil
}

// IsPathWithinRoot returns true iff target resolves inside root (or equals root) with no sensitive segments.
func IsPathWithinRoot(target, root string, allowEnvExample bool) bool {
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.VolumeName(target), filepath.VolumeName(root)) {
			return false
		}
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel != "." {
		for _, seg := range strings.Split(rel, string(filepath.Separator)) {
			if seg == ".." || IsSensitiveSegment(seg, allowEnvExample) {
				return false
			}
		}
	}
	return true
}

func validateCommonRaw(raw string, allowEnvExample bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') {
		return NewError(ErrPathConfinement, "GRAB-E401 parity: empty or null path is denied", "Provide a clean local directory or relative path.")
	}
	if strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, "//") {
		return NewError(ErrPathConfinement, "GRAB-E401 parity: UNC or network share paths are denied", "Use a local filesystem path.")
	}
	if HasBlockedSegments(trimmed, allowEnvExample) {
		return NewError(
			ErrPathConfinement,
			fmt.Sprintf("GRAB-E401 parity: access to sensitive or parent-traversal path %q is denied", trimmed),
			"Avoid '..', '.git', '.env', '.ssh', '*.key', '*.pem', 'id_rsa', or 'id_ed25519' paths.",
		)
	}
	return nil
}

// ResolveRoot canonicalizes an existing directory and enforces GRAB-E401 path rules.
func ResolveRoot(dir string) (string, error) {
	ws := strings.TrimSpace(dir)
	if ws == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", WrapError(ErrPathConfinement, "unable to resolve current working directory", "Pass --dir explicitly.", err)
		}
		ws = wd
	} else if err := validateCommonRaw(ws, false); err != nil {
		return "", err
	}
	absWS, err := filepath.Abs(ws)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("invalid directory path %q", ws), "Provide a valid directory path.", err)
	}
	realWS, err := filepath.EvalSymlinks(absWS)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("directory %q does not exist", ws), "Create the directory or verify --dir.", err)
	}
	st, err := os.Stat(realWS)
	if err != nil || !st.IsDir() {
		return "", NewError(ErrPathConfinement, fmt.Sprintf("path %q must be an existing directory", ws), "Provide a directory path.")
	}
	return realWS, nil
}

// InitRoot validates and creates (if absent) a target workspace/state directory safely.
func InitRoot(dir string) (string, error) {
	ws := strings.TrimSpace(dir)
	if ws == "" {
		return ResolveRoot("")
	}
	if err := validateCommonRaw(ws, false); err != nil {
		return "", err
	}
	absWS, err := filepath.Abs(ws)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("invalid directory path %q", ws), "Provide a valid directory path.", err)
	}
	realWS, err := EvalExistingSymlinks(absWS)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("directory %q cannot be resolved", ws), "Verify parent directory permissions and symlink targets.", err)
	}
	for _, seg := range strings.FieldsFunc(realWS, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || IsSensitiveSegment(seg, false) {
			return "", NewError(ErrPathConfinement, fmt.Sprintf("target directory %q resolves into sensitive segment %q", ws, seg), "Choose a clean non-sensitive directory.")
		}
	}
	if st, statErr := os.Stat(realWS); statErr == nil {
		if !st.IsDir() {
			return "", NewError(ErrPathConfinement, fmt.Sprintf("target %q exists and is not a directory", ws), "Specify a directory path.")
		}
		return realWS, nil
	} else if !os.IsNotExist(statErr) {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("cannot access %q", ws), "Check filesystem permissions.", statErr)
	}
	if err := os.MkdirAll(realWS, 0o750); err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("failed to create directory %q", ws), "Check directory permissions.", err)
	}
	return filepath.EvalSymlinks(realWS)
}

// ResolveFile confines a file path strictly within rootDir and enforces GRAB-E401 rules.
func ResolveFile(rootDir, rawPath string, opts ResolveOptions) (string, string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if err := validateCommonRaw(trimmed, opts.AllowEnvExample); err != nil {
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, "invalid workspace root", "Provide an existing directory.", err)
	}
	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, fmt.Sprintf("invalid path %q", rawPath), "Use a clean relative path.", err)
	}
	realTarget, err := EvalExistingSymlinks(absCandidate)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, fmt.Sprintf("path %q cannot be resolved", rawPath), "Ensure symlinks stay inside root.", err)
	}
	if !IsPathWithinRoot(realTarget, realRoot, opts.AllowEnvExample) || realTarget == realRoot {
		return "", "", NewError(
			ErrPathConfinement,
			fmt.Sprintf("path %q escapes workspace root %q", rawPath, realRoot),
			"Confine all reads and writes inside the target directory.",
		)
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil || rel == "." {
		return "", "", NewError(ErrPathConfinement, fmt.Sprintf("invalid relative path %q", rawPath), "Specify a file inside the directory.")
	}
	slashRel := filepath.ToSlash(rel)

	st, statErr := os.Stat(realTarget)
	if statErr != nil {
		if !opts.AllowMissing || !os.IsNotExist(statErr) {
			return "", "", WrapError(ErrPathConfinement, fmt.Sprintf("file %q is not accessible", slashRel), "Verify the file exists and is readable.", statErr)
		}
	} else {
		if !st.Mode().IsRegular() {
			return "", "", NewError(ErrPathConfinement, fmt.Sprintf("%q is not a regular file", slashRel), "Point to a regular file.")
		}
		if st.Size() > MaxReadBytes {
			return "", "", NewError(ErrPathConfinement, fmt.Sprintf("%q exceeds 10 MiB read limit", slashRel), "Reduce file size below 10 MiB.")
		}
	}
	return realTarget, slashRel, nil
}

// ResolveDir confines an existing subdirectory strictly inside rootDir.
func ResolveDir(rootDir, rawPath string) (string, string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if err := validateCommonRaw(trimmed, false); err != nil {
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, "invalid workspace root", "Provide an existing directory.", err)
	}
	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, fmt.Sprintf("invalid directory path %q", rawPath), "Use a clean relative path.", err)
	}
	realTarget, err := EvalExistingSymlinks(absCandidate)
	if err != nil {
		return "", "", WrapError(ErrPathConfinement, fmt.Sprintf("directory %q cannot be resolved", rawPath), "Ensure symlinks stay inside root.", err)
	}
	if !IsPathWithinRoot(realTarget, realRoot, false) || realTarget == realRoot {
		return "", "", NewError(ErrPathConfinement, fmt.Sprintf("directory %q escapes root %q", rawPath, realRoot), "Confine directory scans inside the root.")
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil || rel == "." {
		return "", "", NewError(ErrPathConfinement, fmt.Sprintf("invalid relative directory %q", rawPath), "Specify a subdirectory.")
	}
	st, statErr := os.Stat(realTarget)
	if statErr != nil {
		return "", "", statErr
	}
	if !st.IsDir() {
		return "", "", NewError(ErrPathConfinement, fmt.Sprintf("%q is not a directory", filepath.ToSlash(rel)), "Point to a directory.")
	}
	return realTarget, filepath.ToSlash(rel), nil
}

// ResolveExternalReadFile validates a user-supplied file outside the current root with GRAB-E401 checks.
func ResolveExternalReadFile(rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if err := validateCommonRaw(trimmed, false); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("invalid file path %q", rawPath), "Provide a valid file path.", err)
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("file %q does not exist or cannot be resolved", rawPath), "Verify the file path.", err)
	}
	for _, seg := range strings.FieldsFunc(realPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || IsSensitiveSegment(seg, false) {
			return "", NewError(ErrPathConfinement, fmt.Sprintf("target %q resolves into sensitive path segment %q", rawPath, seg), "Never reference .git, .env, .ssh, *.key, or *.pem files.")
		}
	}
	st, err := os.Stat(realPath)
	if err != nil {
		return "", WrapError(ErrPathConfinement, fmt.Sprintf("cannot stat %q", rawPath), "Check file permissions.", err)
	}
	if !st.Mode().IsRegular() {
		return "", NewError(ErrPathConfinement, fmt.Sprintf("%q is not a regular file", rawPath), "Provide a regular file.")
	}
	if st.Size() > MaxReadBytes {
		return "", NewError(ErrPathConfinement, fmt.Sprintf("%q exceeds 10 MiB read limit", rawPath), "Reduce file size below 10 MiB.")
	}
	return realPath, nil
}

// ReadBoundedFile reads a verified regular file strictly capped at MaxReadBytes.
func ReadBoundedFile(absPath string) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, NewError(ErrPathConfinement, "target is not a regular file", "Point to a regular file.")
	}
	if st.Size() > MaxReadBytes {
		return nil, NewError(ErrPathConfinement, "file exceeds 10 MiB read limit", "Provide a smaller file.")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReadBytes {
		return nil, NewError(ErrPathConfinement, "file exceeds 10 MiB read limit", "Provide a smaller file.")
	}
	return data, nil
}

// WriteFileAtomic writes data safely inside a pre-confined rootDir path via temp file + rename.
func WriteFileAtomic(rootDir, relPath string, data []byte, perm os.FileMode, opts ResolveOptions) error {
	opts.AllowMissing = true
	absTarget, _, err := ResolveFile(rootDir, relPath, opts)
	if err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	parent := filepath.Dir(absTarget)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	realRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return err
	}
	realTarget, err := EvalExistingSymlinks(absTarget)
	if err != nil {
		return err
	}
	if !IsPathWithinRoot(realTarget, realRoot, opts.AllowEnvExample) {
		return NewError(ErrPathConfinement, fmt.Sprintf("write target %q escapes workspace", relPath), "Keep all writes inside the workspace.")
	}
	tmp, err := os.CreateTemp(parent, ".grabtac-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, absTarget); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// SHA256Bytes returns lowercase hex SHA-256 of bytes.
func SHA256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SHA256File returns lowercase hex SHA-256 of a bounded regular file.
func SHA256File(absPath string) (string, error) {
	data, err := ReadBoundedFile(absPath)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(data), nil
}

// CanonicalJSON renders deterministic indented JSON without HTML escaping.
func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
