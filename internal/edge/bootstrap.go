package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"grabtaculous/internal/safepath"
)

const (
	// NginxSigningKeyURL is the official nginx.org PGP signing key URL.
	NginxSigningKeyURL = "https://nginx.org/keys/nginx_signing.key"
	// NginxSigningKeyFingerprint is the pinned official nginx.org PGP signing key fingerprint.
	NginxSigningKeyFingerprint = "573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62"
	// NginxKeyringPath is the Ubuntu 24.04 (noble) APT keyring path for nginx.org.
	NginxKeyringPath = "/usr/share/keyrings/nginx-archive-keyring.gpg"
	// NginxAptListPath is the Ubuntu 24.04 (noble) official nginx.org stable repository file.
	NginxAptListPath = "/etc/apt/sources.list.d/nginx.list"
	// NginxAptPrefPath pins nginx.org packages above Ubuntu universe builds.
	NginxAptPrefPath = "/etc/apt/preferences.d/99nginx"
	// NginxQuicSysctlPath is the kernel QUIC socket buffer tuning drop-in.
	NginxQuicSysctlPath = "/etc/sysctl.d/99-nginx-quic.conf"
	// MinNginxMajor, MinNginxMinor, MinNginxPatch enforce official NGINX >= 1.30.1 (target 1.30.6 Stable).
	MinNginxMajor = 1
	MinNginxMinor = 30
	MinNginxPatch = 1
)

var (
	reNginxVersion    = regexp.MustCompile(`nginx/(\d+)\.(\d+)\.(\d+)`)
	reModulesInclude  = regexp.MustCompile(`(?im)^\s*include\s+/etc/nginx/modules-enabled/\*\.conf\s*;\s*$`)
	reFastCGISnippet  = regexp.MustCompile(`(?im)^(\s*)include\s+snippets/fastcgi-php\.conf\s*;\s*$`)
	reLegacyHTTP2Line = regexp.MustCompile(`(?im)^(\s*)listen\s+(\[::\]:)?443\s+ssl\s+http2\s*;\s*$`)
)

// CommandRunner executes structured argv commands without any shell string interpolation (ADR-017).
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the default production CommandRunner backed by exec.CommandContext.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// BootstrapStep describes one deterministic structured-argv provisioning command.
type BootstrapStep struct {
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Description string   `json:"description"`
}

// BootstrapOptions configures the NGINX 1.24.x -> official nginx.org 1.30.6 host upgrade.
type BootstrapOptions struct {
	NginxConfigDir  string        `json:"nginx_config_dir"`
	StateDir        string        `json:"state_dir"`
	KeyringPath     string        `json:"keyring_path"`
	AptListPath     string        `json:"apt_list_path"`
	AptPrefPath     string        `json:"apt_pref_path"`
	SysctlPath      string        `json:"sysctl_path"`
	DistroCodename  string        `json:"distro_codename"`
	DryRun          bool          `json:"dry_run"`
	SkipUFW         bool          `json:"skip_ufw"`
	Runner          CommandRunner `json:"-"`
}

// BootstrapPlan describes all structured-argv steps and generated repository/sysctl files.
type BootstrapPlan struct {
	SchemaVersion      string          `json:"schema_version"`
	DistroCodename     string          `json:"distro_codename"`
	PinnedFingerprint  string          `json:"pinned_fingerprint"`
	AptRepositoryLine  string          `json:"apt_repository_line"`
	AptPreferenceBody  string          `json:"apt_preference_body"`
	QuicSysctlBody     string          `json:"quic_sysctl_body"`
	LegacyPurgePkgs    []string        `json:"legacy_purge_packages"`
	PublicFirewallPorts []string       `json:"public_firewall_ports"`
	BlockedPublicPorts []string        `json:"blocked_public_ports"`
	Steps              []BootstrapStep `json:"steps"`
}

// BootstrapResult captures the outcome of running ExecuteNginx130Bootstrap.
type BootstrapResult struct {
	SchemaVersion     string          `json:"schema_version"`
	DryRun            bool            `json:"dry_run"`
	SnapshotID        string          `json:"snapshot_id,omitempty"`
	SnapshotManifest  string          `json:"snapshot_manifest,omitempty"`
	SanitizedFiles    []string        `json:"sanitized_files,omitempty"`
	NginxVersion      string          `json:"nginx_version,omitempty"`
	ExecutedSteps     []BootstrapStep `json:"executed_steps"`
	Plan              *BootstrapPlan  `json:"plan"`
}

// BuildNginx130BootstrapPlan builds the deterministic, zero-shell-interpolation Ubuntu 24.04 upgrade plan.
func BuildNginx130BootstrapPlan(opts BootstrapOptions) *BootstrapPlan {
	codename := strings.TrimSpace(opts.DistroCodename)
	if codename == "" {
		codename = "noble"
	}
	keyring := strings.TrimSpace(opts.KeyringPath)
	if keyring == "" {
		keyring = NginxKeyringPath
	}

	aptLine := fmt.Sprintf("deb [signed-by=%s] https://nginx.org/packages/ubuntu %s nginx\n", keyring, codename)
	aptPref := "Package: *\nPin: origin nginx.org\nPin: release o=nginx\nPin-Priority: 900\n"
	purgePkgs := []string{"nginx", "nginx-common", "nginx-core", "nginx-full", "nginx-light", "libnginx-mod-*"}
	publicPorts := []string{"80/tcp", "443/tcp", "443/udp"}
	blockedPorts := []string{"8765:8772/tcp", "8090/tcp", "9000/tcp", "5432/tcp", "3306/tcp", "6379/tcp", "9440/tcp"}

	steps := []BootstrapStep{
		{
			Name:        "stop-legacy-nginx",
			Command:     "systemctl",
			Args:        []string{"stop", "nginx"},
			Description: "Stop stock Ubuntu nginx 1.24.x service before package replacement",
		},
		{
			Name:        "purge-stock-nginx-124",
			Command:     "apt-get",
			Args:        append([]string{"remove", "--purge", "-y"}, purgePkgs...),
			Description: "Remove stock Ubuntu nginx 1.24.x, nginx-common, nginx-core, and libnginx-mod-* packages",
		},
		{
			Name:        "apt-autoremove",
			Command:     "apt-get",
			Args:        []string{"autoremove", "-y"},
			Description: "Clean unused Debian nginx dependency packages",
		},
		{
			Name:        "install-apt-prereqs",
			Command:     "apt-get",
			Args:        []string{"install", "-y", "curl", "gnupg2", "ca-certificates", "lsb-release", "ubuntu-keyring"},
			Description: "Ensure GPG and HTTPS APT transport tooling is present",
		},
		{
			Name:        "fetch-nginx-pgp-key",
			Command:     "curl",
			Args:        []string{"-fsSL", NginxSigningKeyURL, "-o", keyring + ".asc"},
			Description: "Download official nginx.org PGP signing key",
		},
		{
			Name:        "verify-nginx-pgp-fingerprint",
			Command:     "gpg",
			Args:        []string{"--show-keys", "--with-colons", keyring + ".asc"},
			Description: "Verify pinned nginx.org PGP fingerprint " + NginxSigningKeyFingerprint,
		},
		{
			Name:        "dearmor-nginx-keyring",
			Command:     "gpg",
			Args:        []string{"--batch", "--yes", "--dearmor", "-o", keyring, keyring + ".asc"},
			Description: "Convert verified nginx.org ASCII key into APT binary keyring",
		},
		{
			Name:        "apt-update-nginx-org",
			Command:     "apt-get",
			Args:        []string{"update"},
			Description: "Refresh package metadata from pinned official nginx.org stable repo",
		},
		{
			Name:        "install-official-nginx-130",
			Command:     "apt-get",
			Args:        []string{"install", "-y", "nginx"},
			Description: "Install official nginx.org NGINX 1.30.6 (Stable) with native QUIC/HTTP/3",
		},
		{
			Name:        "apply-quic-sysctl",
			Command:     "sysctl",
			Args:        []string{"--system"},
			Description: "Apply 99-nginx-quic.conf UDP rmem_max/wmem_max=7500000 and somaxconn=8192",
		},
	}

	if !opts.SkipUFW {
		for _, port := range publicPorts {
			steps = append(steps, BootstrapStep{
				Name:        "ufw-allow-" + strings.ReplaceAll(port, "/", "-"),
				Command:     "ufw",
				Args:        []string{"allow", port},
				Description: "Allow public edge port " + port + " (HTTP/HTTPS/QUIC)",
			})
		}
		for _, blocked := range blockedPorts {
			steps = append(steps, BootstrapStep{
				Name:        "ufw-deny-" + strings.NewReplacer("/", "-", ":", "-").Replace(blocked),
				Command:     "ufw",
				Args:        []string{"deny", "in", "to", "any", "port", strings.Split(blocked, "/")[0], "proto", "tcp"},
				Description: "Ensure internal worker/datastore port " + blocked + " stays strictly off public interfaces",
			})
		}
	}

	steps = append(steps,
		BootstrapStep{
			Name:        "nginx-version-check",
			Command:     "nginx",
			Args:        []string{"-v"},
			Description: "Verify installed NGINX version >= 1.30.1 (1.30.6 Stable)",
		},
		BootstrapStep{
			Name:        "nginx-config-test",
			Command:     "nginx",
			Args:        []string{"-t"},
			Description: "Validate NGINX 1.30.6 configuration syntax before reload",
		},
		BootstrapStep{
			Name:        "enable-start-nginx",
			Command:     "systemctl",
			Args:        []string{"enable", "--now", "nginx"},
			Description: "Enable and start official nginx.org 1.30.6 service",
		},
	)

	return &BootstrapPlan{
		SchemaVersion:       "1.0.0",
		DistroCodename:      codename,
		PinnedFingerprint:   NginxSigningKeyFingerprint,
		AptRepositoryLine:   aptLine,
		AptPreferenceBody:   aptPref,
		QuicSysctlBody:      string(RenderQuicSysctlConf()),
		LegacyPurgePkgs:     purgePkgs,
		PublicFirewallPorts: publicPorts,
		BlockedPublicPorts:  blockedPorts,
		Steps:               steps,
	}
}

// SanitizeLegacyNginxConfig strips Ubuntu/Debian-specific includes and deprecated 1.24 directives
// that break official nginx.org 1.30.6 packages.
func SanitizeLegacyNginxConfig(content string) (string, bool) {
	orig := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(orig, "\n")
	changed := false
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		if reModulesInclude.MatchString(line) {
			changed = true
			out = append(out, "# [grabtaculous] stripped legacy Debian modules-enabled include for official nginx.org 1.30.6")
			continue
		}
		if m := reFastCGISnippet.FindStringSubmatch(line); len(m) == 2 {
			changed = true
			indent := m[1]
			out = append(out, indent+"include /etc/nginx/fastcgi_params;")
			out = append(out, indent+"fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;")
			continue
		}
		if m := reLegacyHTTP2Line.FindStringSubmatch(line); len(m) == 3 {
			changed = true
			indent := m[1]
			v6 := m[2]
			out = append(out, fmt.Sprintf("%slisten %s443 ssl;", indent, v6))
			out = append(out, fmt.Sprintf("%shttp2 on;", indent))
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed
}

// SanitizeLegacyNginxDir walks nginxDir and repairs legacy Debian/Ubuntu 1.24.x config lines atomically.
func SanitizeLegacyNginxDir(nginxDir string) ([]string, error) {
	root, err := safepath.ResolveRoot(nginxDir)
	if err != nil {
		return nil, err
	}
	var modified []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			return wErr
		}
		if d.IsDir() {
			if safepath.IsSensitiveSegment(d.Name(), false) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".conf") {
			return nil
		}
		rel, rErr := filepath.Rel(root, p)
		if rErr != nil {
			return rErr
		}
		relSlash := filepath.ToSlash(rel)
		if safepath.HasBlockedSegments(relSlash, false) {
			return nil
		}
		raw, readErr := safepath.ReadBoundedFile(p)
		if readErr != nil {
			return readErr
		}
		cleaned, changed := SanitizeLegacyNginxConfig(string(raw))
		if changed {
			if err := safepath.WriteFileAtomic(root, relSlash, []byte(cleaned), 0o644, safepath.ResolveOptions{}); err != nil {
				return err
			}
			modified = append(modified, relSlash)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(modified)
	return modified, nil
}

// ParseNginxVersion parses `nginx version: nginx/1.30.6` stderr/stdout output.
func ParseNginxVersion(output string) (int, int, int, error) {
	m := reNginxVersion.FindStringSubmatch(output)
	if len(m) != 4 {
		return 0, 0, 0, safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("unable to parse nginx version from %q", strings.TrimSpace(output)), "Verify official nginx.org binary is installed.")
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return maj, min, pat, nil
}

// VerifyNginxVersionMin ensures the installed NGINX binary meets at least minMajor.minMinor.minPatch.
func VerifyNginxVersionMin(versionOutput string, minMajor, minMinor, minPatch int) error {
	maj, min, pat, err := ParseNginxVersion(versionOutput)
	if err != nil {
		return err
	}
	if maj < minMajor || (maj == minMajor && min < minMinor) || (maj == minMajor && min == minMinor && pat < minPatch) {
		return safepath.NewError(
			safepath.ErrSecurityPolicy,
			fmt.Sprintf("installed nginx %d.%d.%d is older than required official nginx.org %d.%d.%d+", maj, min, pat, minMajor, minMinor, minPatch),
			"Purge stock Ubuntu nginx 1.24.x and install official nginx.org >= 1.30.1 (1.30.6 Stable).",
		)
	}
	return nil
}

// ExecuteNginx130Bootstrap snapshots existing /etc/nginx, strips legacy 1.24.x syntax,
// verifies the nginx.org PGP fingerprint, writes pinned repo + QUIC sysctl configs, and runs argv steps.
func ExecuteNginx130Bootstrap(ctx context.Context, opts BootstrapOptions) (*BootstrapResult, error) {
	nginxDir := strings.TrimSpace(opts.NginxConfigDir)
	if nginxDir == "" {
		nginxDir = "/etc/nginx"
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(nginxDir, ".grabtaculous")
	}
	if strings.TrimSpace(opts.KeyringPath) == "" {
		opts.KeyringPath = NginxKeyringPath
	}
	if strings.TrimSpace(opts.AptListPath) == "" {
		opts.AptListPath = NginxAptListPath
	}
	if strings.TrimSpace(opts.AptPrefPath) == "" {
		opts.AptPrefPath = NginxAptPrefPath
	}
	if strings.TrimSpace(opts.SysctlPath) == "" {
		opts.SysctlPath = NginxQuicSysctlPath
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner
	}

	plan := BuildNginx130BootstrapPlan(opts)
	res := &BootstrapResult{
		SchemaVersion: "1.0.0",
		DryRun:        opts.DryRun,
		Plan:          plan,
	}

	// Capture SHA-256 verified snapshot of existing nginx config directory if present.
	if st, err := os.Stat(nginxDir); err == nil && st.IsDir() {
		snapID, manifestPath, snapErr := snapshotNginxDir(nginxDir, stateDir)
		if snapErr != nil {
			return nil, snapErr
		}
		res.SnapshotID = snapID
		res.SnapshotManifest = manifestPath
	}

	if opts.DryRun {
		res.ExecutedSteps = append([]BootstrapStep(nil), plan.Steps...)
		return res, nil
	}

	// Ensure /etc/nginx/sites-available, sites-enabled, and conf.d exist when upgrading.
	for _, sub := range []string{"conf.d", "sites-available", "sites-enabled"} {
		if err := os.MkdirAll(filepath.Join(nginxDir, sub), 0o755); err != nil {
			return nil, safepath.WrapError(safepath.ErrSecurityPolicy, fmt.Sprintf("failed creating %s", filepath.Join(nginxDir, sub)), "Run as a privileged bootstrap operator or check directory permissions.", err)
		}
	}

	// Sanitize any existing legacy modules-enabled / fastcgi-php.conf / listen 443 ssl http2 lines.
	if _, err := os.Stat(nginxDir); err == nil {
		sanitized, sErr := SanitizeLegacyNginxDir(nginxDir)
		if sErr != nil {
			return nil, sErr
		}
		res.SanitizedFiles = sanitized
	}

	for _, step := range plan.Steps {
		if step.Name == "apt-update-nginx-org" {
			if err := writeBootstrapFile(opts.AptListPath, []byte(plan.AptRepositoryLine)); err != nil {
				return nil, err
			}
			if err := writeBootstrapFile(opts.AptPrefPath, []byte(plan.AptPreferenceBody)); err != nil {
				return nil, err
			}
		}
		if step.Name == "apply-quic-sysctl" {
			if err := writeBootstrapFile(opts.SysctlPath, []byte(plan.QuicSysctlBody)); err != nil {
				return nil, err
			}
		}

		out, runErr := runner(ctx, step.Command, step.Args...)
		if runErr != nil {
			// Best-effort service stop may return non-zero if nginx is not yet installed.
			if step.Name == "stop-legacy-nginx" {
				res.ExecutedSteps = append(res.ExecutedSteps, step)
				continue
			}
			return nil, safepath.WrapError(
				safepath.ErrSecurityPolicy,
				fmt.Sprintf("nginx 1.30.6 bootstrap step %q failed: %s", step.Name, strings.TrimSpace(string(out))),
				"Inspect Pre-Deploy Snapshot in "+res.SnapshotManifest+" and repair host package state.",
				runErr,
			)
		}
		if step.Name == "verify-nginx-pgp-fingerprint" {
			if !strings.Contains(strings.ToUpper(string(out)), NginxSigningKeyFingerprint) {
				return nil, safepath.NewError(
					safepath.ErrSecurityPolicy,
					fmt.Sprintf("nginx.org PGP fingerprint mismatch (expected %s)", NginxSigningKeyFingerprint),
					"Aborting installation because downloaded signing key did not match pinned nginx.org fingerprint.",
				)
			}
		}
		if step.Name == "nginx-version-check" {
			if err := VerifyNginxVersionMin(string(out), MinNginxMajor, MinNginxMinor, MinNginxPatch); err != nil {
				return nil, err
			}
			res.NginxVersion = strings.TrimSpace(string(out))
		}
		res.ExecutedSteps = append(res.ExecutedSteps, step)
	}

	return res, nil
}

func writeBootstrapFile(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)
	root, err := safepath.InitRoot(dir)
	if err != nil {
		return err
	}
	return safepath.WriteFileAtomic(root, base, data, 0o644, safepath.ResolveOptions{})
}

type nginxSnapshotEntry struct {
	RelPath string `json:"rel_path"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
}

func snapshotNginxDir(nginxDir, stateDir string) (string, string, error) {
	realSrc, err := safepath.ResolveRoot(nginxDir)
	if err != nil {
		return "", "", err
	}
	stateRoot, err := safepath.InitRoot(stateDir)
	if err != nil {
		return "", "", err
	}
	snapID := fmt.Sprintf("nginx-pre-%s", time.Now().UTC().Format("20060102T150405Z"))
	snapRel := filepath.ToSlash(filepath.Join("nginx-snapshots", snapID))
	snapAbs := filepath.Join(stateRoot, filepath.FromSlash(snapRel))
	if err := os.MkdirAll(filepath.Join(snapAbs, "payload"), 0o750); err != nil {
		return "", "", err
	}

	var entries []nginxSnapshotEntry
	err = filepath.WalkDir(realSrc, func(p string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			return wErr
		}
		if d.IsDir() {
			if safepath.IsSensitiveSegment(d.Name(), false) || d.Name() == ".grabtaculous" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rErr := filepath.Rel(realSrc, p)
		if rErr != nil {
			return rErr
		}
		relSlash := filepath.ToSlash(rel)
		if safepath.HasBlockedSegments(relSlash, false) {
			return nil
		}
		data, readErr := safepath.ReadBoundedFile(p)
		if readErr != nil {
			return readErr
		}
		if err := safepath.WriteFileAtomic(filepath.Join(snapAbs, "payload"), relSlash, data, 0o644, safepath.ResolveOptions{}); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entries = append(entries, nginxSnapshotEntry{
			RelPath: relSlash,
			SHA256:  hex.EncodeToString(sum[:]),
			Bytes:   int64(len(data)),
		})
		return nil
	})
	if err != nil {
		return "", "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RelPath < entries[j].RelPath })
	doc := map[string]any{
		"schema_version": "1.0.0",
		"snapshot_id":    snapID,
		"source_dir":     realSrc,
		"created_at_utc": time.Now().UTC().Format(time.RFC3339),
		"files":          entries,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := safepath.WriteFileAtomic(snapAbs, "manifest.json", raw, 0o644, safepath.ResolveOptions{}); err != nil {
		return "", "", err
	}
	return snapID, filepath.Join(snapAbs, "manifest.json"), nil
}
