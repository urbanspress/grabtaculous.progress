package container

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"grabtaculous/internal/safepath"
)

// HealthcheckSpec defines a container-level health probe configuration.
type HealthcheckSpec struct {
	Test        []string `json:"test"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	Retries     int      `json:"retries,omitempty"`
	StartPeriod string   `json:"start_period,omitempty"`
}

// ServiceSpec defines a hardened container service inside a Grabtaculous Compose stack.
type ServiceSpec struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Container   string            `json:"container_name,omitempty"`
	ReadOnly    bool              `json:"read_only"`
	User        string            `json:"user"`
	CapDrop     []string          `json:"cap_drop"`
	SecurityOpt []string          `json:"security_opt"`
	Privileged  bool              `json:"privileged,omitempty"`
	NetworkMode string            `json:"network_mode,omitempty"`
	Tmpfs       []string          `json:"tmpfs,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	Networks    []string          `json:"networks,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Command     []string          `json:"command,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Healthcheck *HealthcheckSpec  `json:"healthcheck,omitempty"`
	Restart     string            `json:"restart,omitempty"`
	IsDataStore bool              `json:"is_data_store,omitempty"`
	IsEdgeProxy bool              `json:"is_edge_proxy,omitempty"`
}

// StackSpec defines a complete multi-service deployment stack.
type StackSpec struct {
	StackName       string        `json:"stack_name"`
	EdgeProxy       string        `json:"edge_proxy"`
	Domain          string        `json:"domain,omitempty"`
	Services        []ServiceSpec `json:"services"`
	Volumes         []string      `json:"volumes,omitempty"`
	InternalNetwork bool          `json:"internal_network,omitempty"`
}

// AuditOptions configures container admission checks.
type AuditOptions struct {
	RequireDigest       bool
	AllowNon65532AppUID bool
	AllowNon65532DBUID  bool
}

var (
	reSHA256Digest  = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)
	reNumericUIDGID = regexp.MustCompile(`^(\d+)(?::(\d+))?$`)
	reSecretKey     = regexp.MustCompile(`(?i)(?:^|_)(?:MASTER_KEY|SECRET|PASSWORD|PASSWD|API_KEY|ACCESS_KEY|PRIVATE_KEY|TOKEN|AUTH_KEY|WEBHOOK_SECRET|SIGNING_KEY|DB_PASS)(?:$|_)`)
	reSecretAssign  = regexp.MustCompile(`(?i)^\s*(?:-\s*)?([A-Z0-9_]*(?:MASTER_KEY|SECRET|PASSWORD|PASSWD|API_KEY|ACCESS_KEY|PRIVATE_KEY|TOKEN|AUTH_KEY|WEBHOOK_SECRET|SIGNING_KEY|DB_PASS)[A-Z0-9_]*)\s*[:=]\s*(.+?)\s*$`)
	reDSNPassword   = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^:\s/]+:([^@\s$}]+)@`)
	rePrivateIPv4   = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)
	reEnvDefault    = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*):-(.*)\}$`)
)

// ValidateStackSpec enforces GRABTAC-E401 fail-closed hardening across a StackSpec.
func ValidateStackSpec(stack StackSpec, opts AuditOptions) error {
	var issues []string
	if strings.TrimSpace(stack.EdgeProxy) != "" {
		switch stack.EdgeProxy {
		case "caddy", "nginx", "cloudflare-tunnel":
		default:
			issues = append(issues, fmt.Sprintf("unsupported edge_proxy %q (expected caddy, nginx, or cloudflare-tunnel)", stack.EdgeProxy))
		}
	}
	if len(stack.Services) == 0 {
		issues = append(issues, "stack defines zero services")
	}
	for _, svc := range stack.Services {
		issues = append(issues, ValidateServiceSpec(svc, opts)...)
	}
	if len(issues) > 0 {
		return safepath.NewError(
			safepath.ErrSecurityPolicy,
			strings.Join(issues, " "),
			"Enforce read_only: true, cap_drop: [ALL], no-new-privileges:true, non-root UID 65532:65532, noexec/nosuid tmpfs, pinned @sha256 digests (no :latest), 127.0.0.1 loopback ports, and ${VAR} env placeholders.",
		)
	}
	return nil
}

// ValidateServiceSpec validates a single container service against GRABTAC-E401 invariants.
func ValidateServiceSpec(svc ServiceSpec, opts AuditOptions) []string {
	var issues []string
	name := strings.TrimSpace(svc.Name)
	if name == "" {
		name = "<unnamed>"
	}

	if !svc.ReadOnly {
		issues = append(issues, fmt.Sprintf("Service %q must enforce read_only: true.", name))
	}
	if !containsFold(svc.CapDrop, "ALL") {
		issues = append(issues, fmt.Sprintf("Service %q is missing cap_drop: [ALL].", name))
	}
	if !hasNoNewPrivileges(svc.SecurityOpt) {
		issues = append(issues, fmt.Sprintf("Service %q is missing security_opt: [no-new-privileges:true].", name))
	}
	if svc.Privileged {
		issues = append(issues, fmt.Sprintf("Service %q enables forbidden privileged: true.", name))
	}
	if strings.EqualFold(strings.TrimSpace(svc.NetworkMode), "host") {
		issues = append(issues, fmt.Sprintf("Service %q uses forbidden network_mode: host.", name))
	}

	if msg := validateServiceUser(name, svc, opts); msg != "" {
		issues = append(issues, msg)
	}
	if !hasHardenedTmpfs(svc.Tmpfs) {
		issues = append(issues, fmt.Sprintf("Service %q must configure /tmp tmpfs with noexec and nosuid.", name))
	}
	if msg := validateServiceImage(name, svc.Image, opts.RequireDigest); msg != "" {
		issues = append(issues, msg)
	}
	for _, vol := range svc.Volumes {
		if msg := validateVolumeMount(name, vol); msg != "" {
			issues = append(issues, msg)
		}
	}
	for _, port := range svc.Ports {
		if isUnsafePortMapping(port) {
			issues = append(issues, fmt.Sprintf("Service %q binds non-loopback port %q (must bind strictly to 127.0.0.1:... or [::1]:...).", name, port))
		}
	}

	envKeys := make([]string, 0, len(svc.Environment))
	for k := range svc.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		v := svc.Environment[k]
		if reSecretKey.MatchString(k) && !isSafeSecretReference(v) {
			issues = append(issues, fmt.Sprintf("Service %q contains plaintext secret in environment variable %s (value redacted).", name, k))
		}
		if hasEmbeddedPassword(v) {
			issues = append(issues, fmt.Sprintf("Service %q contains embedded credential in DSN %s (value redacted).", name, k))
		}
		if hasPrivateNonLoopbackIPv4(v) {
			issues = append(issues, fmt.Sprintf("Service %q contains hardcoded private IPv4 literal in %s (redacted).", name, k))
		}
	}
	for _, arg := range svc.Command {
		if hasEmbeddedPassword(arg) {
			issues = append(issues, fmt.Sprintf("Service %q contains embedded credential in command argument (value redacted).", name))
		}
		if hasPrivateNonLoopbackIPv4(arg) {
			issues = append(issues, fmt.Sprintf("Service %q contains hardcoded private IPv4 literal in command argument (redacted).", name))
		}
	}

	return issues
}

func validateServiceUser(name string, svc ServiceSpec, opts AuditOptions) string {
	raw := strings.TrimSpace(svc.User)
	if raw == "" {
		return fmt.Sprintf("Service %q is missing explicit non-root user (expected 65532:65532).", name)
	}
	lower := strings.ToLower(raw)
	if lower == "root" || strings.HasPrefix(lower, "root:") || strings.HasSuffix(lower, ":root") {
		return fmt.Sprintf("Service %q cannot run as root (%q).", name, raw)
	}
	m := reNumericUIDGID.FindStringSubmatch(raw)
	if m == nil {
		return fmt.Sprintf("Service %q must use numeric non-root UID:GID (got %q).", name, raw)
	}
	uid, _ := strconv.Atoi(m[1])
	gid := uid
	if m[2] != "" {
		gid, _ = strconv.Atoi(m[2])
	}
	if uid == 0 || gid == 0 {
		return fmt.Sprintf("Service %q cannot use UID/GID 0 (%q).", name, raw)
	}
	if raw == "65532:65532" {
		return ""
	}
	if svc.IsDataStore && opts.AllowNon65532DBUID {
		return ""
	}
	if !svc.IsDataStore && opts.AllowNon65532AppUID {
		return ""
	}
	return fmt.Sprintf("Service %q must run as hardened non-root UID:GID 65532:65532 (got %q).", name, raw)
}

func validateServiceImage(name, rawImage string, requireDigest bool) string {
	img := strings.TrimSpace(rawImage)
	if img == "" {
		return fmt.Sprintf("Service %q is missing an image reference.", name)
	}
	// Resolve ${VAR:-default} wrapper if present so defaults are strictly audited too.
	if m := reEnvDefault.FindStringSubmatch(img); len(m) == 3 {
		img = strings.TrimSpace(m[2])
	} else if strings.HasPrefix(img, "${") && strings.HasSuffix(img, "}") {
		return fmt.Sprintf("Service %q image %q must declare an explicit pinned default tag and digest.", name, rawImage)
	}

	lower := strings.ToLower(img)
	withoutDigest := img
	hasDigest := false
	if atIdx := strings.Index(img, "@"); atIdx >= 0 {
		digestPart := strings.ToLower(img[atIdx:])
		if !reSHA256Digest.MatchString(digestPart) {
			return fmt.Sprintf("Service %q image %q has malformed sha256 digest.", name, rawImage)
		}
		hasDigest = true
		withoutDigest = img[:atIdx]
	}

	// Check tag on final path segment.
	lastSlash := strings.LastIndexByte(withoutDigest, '/')
	tagSegment := withoutDigest
	if lastSlash >= 0 {
		tagSegment = withoutDigest[lastSlash+1:]
	}
	colonIdx := strings.IndexByte(tagSegment, ':')
	if colonIdx < 0 {
		return fmt.Sprintf("Service %q image %q is missing an explicit pinned version tag.", name, rawImage)
	}
	tag := strings.ToLower(strings.TrimSpace(tagSegment[colonIdx+1:]))
	if tag == "" || tag == "latest" || strings.Contains(lower, ":latest@") || strings.HasSuffix(lower, ":latest") {
		return fmt.Sprintf("Service %q uses forbidden unpinned ':latest' tag (%q).", name, rawImage)
	}
	if requireDigest && !hasDigest {
		return fmt.Sprintf("Service %q image %q must include a pinned @sha256:<64-hex> digest.", name, rawImage)
	}
	return ""
}

func validateVolumeMount(name, vol string) string {
	trimmed := strings.TrimSpace(vol)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "docker.sock") {
		return fmt.Sprintf("Service %q mounts forbidden Docker socket (%q).", name, vol)
	}
	parts := strings.Split(trimmed, ":")
	src := strings.TrimSpace(parts[0])
	if src == "/" || src == "/etc" || src == "/proc" || src == "/sys" || src == "/root" {
		return fmt.Sprintf("Service %q mounts sensitive host directory %q.", name, src)
	}
	for _, seg := range strings.FieldsFunc(src, func(r rune) bool { return r == '/' || r == '\\' }) {
		if safepath.IsSensitiveSegment(seg, false) {
			return fmt.Sprintf("Service %q mounts sensitive secret/VCS path %q.", name, vol)
		}
	}
	if len(parts) >= 2 {
		dst := strings.TrimSpace(parts[1])
		mode := ""
		if len(parts) >= 3 {
			mode = strings.ToLower(strings.TrimSpace(parts[2]))
		}
		if (dst == "/app/src" || dst == "/app/views" || dst == "/app/themes" || dst == "/workspace") && mode != "ro" {
			return fmt.Sprintf("Service %q mounts immutable source/theme target %q without ':ro'.", name, vol)
		}
	}
	return ""
}

func hasHardenedTmpfs(tmpfs []string) bool {
	for _, entry := range tmpfs {
		trimmed := strings.TrimSpace(entry)
		parts := strings.SplitN(trimmed, ":", 2)
		if strings.TrimSpace(parts[0]) != "/tmp" {
			continue
		}
		if len(parts) < 2 {
			continue
		}
		opts := strings.ToLower(parts[1])
		if strings.Contains(opts, "noexec") && strings.Contains(opts, "nosuid") {
			return true
		}
	}
	return false
}

func hasNoNewPrivileges(opts []string) bool {
	for _, o := range opts {
		trimmed := strings.ToLower(strings.TrimSpace(o))
		if trimmed == "no-new-privileges:true" || trimmed == "no-new-privileges=true" {
			return true
		}
	}
	return false
}

func containsFold(items []string, target string) bool {
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(it), target) {
			return true
		}
	}
	return false
}

func isUnsafePortMapping(mapping string) bool {
	m := strings.TrimSpace(mapping)
	if m == "" {
		return false
	}
	if strings.HasPrefix(m, "127.0.0.1:") || strings.HasPrefix(m, "[::1]:") || strings.HasPrefix(m, "::1:") {
		return false
	}
	return true
}

func isSafeSecretReference(val string) bool {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" || trimmed == `""` || trimmed == `''` || trimmed == "null" {
		return true
	}
	// Reject ${VAR:-hardcoded_default} where default is non-empty!
	if m := reEnvDefault.FindStringSubmatch(trimmed); len(m) == 3 {
		return strings.TrimSpace(m[2]) == ""
	}
	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		return true
	}
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		return true
	}
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "CHANGE_ME") || upper == "REDACTED" {
		return true
	}
	if _, err := strconv.Atoi(trimmed); err == nil {
		return true
	}
	return false
}

func hasEmbeddedPassword(s string) bool {
	m := reDSNPassword.FindStringSubmatch(s)
	if len(m) != 2 {
		return false
	}
	return !strings.HasPrefix(m[1], "${")
}

func hasPrivateNonLoopbackIPv4(s string) bool {
	for _, ipMatch := range rePrivateIPv4.FindAllString(s, -1) {
		ip := net.ParseIP(ipMatch)
		if ip != nil && !ip.IsLoopback() {
			return true
		}
	}
	return false
}

// ScanNoPlaintextSecrets audits arbitrary config/env text for secret literals, embedded DSN credentials,
// and non-loopback private IPv4 addresses, redacting sensitive values in all diagnostics.
func ScanNoPlaintextSecrets(relPath, content string) []string {
	var issues []string
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for idx, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := reSecretAssign.FindStringSubmatch(rawLine); len(m) == 3 {
			keyName := m[1]
			val := trimYAMLQuotes(strings.TrimSpace(strings.TrimSuffix(m[2], ",")))
			if !isSafeSecretReference(val) {
				issues = append(issues, fmt.Sprintf("%s line %d: hardcoded secret value detected for %s (value redacted).", relPath, idx+1, keyName))
			}
		}
		if hasEmbeddedPassword(rawLine) {
			issues = append(issues, fmt.Sprintf("%s line %d: embedded credential in connection URI (value redacted).", relPath, idx+1))
		}
		if hasPrivateNonLoopbackIPv4(rawLine) {
			issues = append(issues, fmt.Sprintf("%s line %d: hardcoded private IPv4 literal detected (redacted).", relPath, idx+1))
		}
	}
	return issues
}

// ParseComposeServices parses a Docker Compose YAML document's `services:` section into ServiceSpecs.
func ParseComposeServices(content string) ([]ServiceSpec, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inServices := false
	servicesIndent := -1
	var services []*ServiceSpec
	var current *ServiceSpec
	currentSection := ""
	sectionIndent := -1

	for _, rawLine := range lines {
		noComment := stripYAMLComment(rawLine)
		if strings.TrimSpace(noComment) == "" {
			continue
		}
		indent := countLeadingSpaces(noComment)
		trimmed := strings.TrimSpace(noComment)

		if trimmed == "services:" {
			inServices = true
			servicesIndent = indent
			current = nil
			currentSection = ""
			continue
		}
		if inServices && indent <= servicesIndent && !strings.HasPrefix(trimmed, "-") {
			inServices = false
			current = nil
			currentSection = ""
			continue
		}
		if !inServices {
			continue
		}

		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
			if current == nil || indent <= servicesIndent+2 {
				svcName := strings.TrimSuffix(trimmed, ":")
				current = &ServiceSpec{
					Name:        svcName,
					Environment: map[string]string{},
				}
				services = append(services, current)
				currentSection = ""
				continue
			}
		}
		if current == nil {
			continue
		}

		// List items under cap_drop, security_opt, tmpfs, volumes, ports, environment, command
		if strings.HasPrefix(trimmed, "-") && currentSection != "" && indent >= sectionIndent {
			item := trimYAMLQuotes(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
			switch currentSection {
			case "cap_drop":
				current.CapDrop = append(current.CapDrop, item)
			case "security_opt":
				current.SecurityOpt = append(current.SecurityOpt, item)
			case "tmpfs":
				current.Tmpfs = append(current.Tmpfs, item)
			case "volumes":
				current.Volumes = append(current.Volumes, item)
			case "ports":
				current.Ports = append(current.Ports, item)
			case "command":
				current.Command = append(current.Command, item)
			case "environment":
				if eqIdx := strings.IndexByte(item, '='); eqIdx > 0 {
					k := strings.TrimSpace(item[:eqIdx])
					v := trimYAMLQuotes(strings.TrimSpace(item[eqIdx+1:]))
					current.Environment[k] = v
				}
			}
			continue
		}

		// Map entries under environment:
		if currentSection == "environment" && indent > sectionIndent {
			if colonIdx := strings.IndexByte(trimmed, ':'); colonIdx > 0 {
				k := strings.TrimSpace(trimmed[:colonIdx])
				v := trimYAMLQuotes(strings.TrimSpace(trimmed[colonIdx+1:]))
				current.Environment[k] = v
				continue
			}
		}

		if colonIdx := strings.IndexByte(trimmed, ':'); colonIdx > 0 {
			key := strings.TrimSpace(trimmed[:colonIdx])
			val := strings.TrimSpace(trimmed[colonIdx+1:])
			valUnquoted := trimYAMLQuotes(val)
			currentSection = ""

			switch key {
			case "cap_drop", "security_opt", "tmpfs", "volumes", "ports", "environment", "command":
				if val == "" {
					currentSection = key
					sectionIndent = indent
				} else if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
					items := splitInlineYAMLList(val)
					switch key {
					case "cap_drop":
						current.CapDrop = append(current.CapDrop, items...)
					case "security_opt":
						current.SecurityOpt = append(current.SecurityOpt, items...)
					case "tmpfs":
						current.Tmpfs = append(current.Tmpfs, items...)
					case "volumes":
						current.Volumes = append(current.Volumes, items...)
					case "ports":
						current.Ports = append(current.Ports, items...)
					case "command":
						current.Command = append(current.Command, items...)
					}
				}
			case "read_only":
				current.ReadOnly = strings.EqualFold(valUnquoted, "true")
			case "privileged":
				current.Privileged = strings.EqualFold(valUnquoted, "true")
			case "network_mode":
				current.NetworkMode = valUnquoted
			case "user":
				current.User = valUnquoted
			case "image":
				current.Image = valUnquoted
			case "working_dir":
				current.WorkingDir = valUnquoted
			}
		}
	}

	if len(services) == 0 {
		return nil, safepath.NewError(safepath.ErrSecurityPolicy, "Compose file defines no services", "Declare at least one hardened service under services:.")
	}
	out := make([]ServiceSpec, 0, len(services))
	for _, s := range services {
		nameLower := strings.ToLower(s.Name)
		if strings.Contains(nameLower, "postgres") || strings.Contains(nameLower, "mariadb") || strings.Contains(nameLower, "redis") {
			s.IsDataStore = true
		}
		if strings.Contains(nameLower, "caddy") || strings.Contains(nameLower, "nginx") || strings.Contains(nameLower, "cloudflared") || strings.Contains(nameLower, "edge-proxy") {
			s.IsEdgeProxy = true
		}
		out = append(out, *s)
	}
	return out, nil
}

// AuditComposeYAML performs full fail-closed GRABTAC-E401 validation on a Docker Compose YAML file.
func AuditComposeYAML(relPath, content string, opts AuditOptions) error {
	services, err := ParseComposeServices(content)
	if err != nil {
		return err
	}
	var issues []string
	for _, svc := range services {
		issues = append(issues, ValidateServiceSpec(svc, opts)...)
	}
	issues = append(issues, ScanNoPlaintextSecrets(relPath, content)...)
	if len(issues) > 0 {
		return safepath.NewError(
			safepath.ErrSecurityPolicy,
			strings.Join(issues, " "),
			fmt.Sprintf("Repair security policy violations in %s before deployment.", relPath),
		)
	}
	return nil
}

func splitInlineYAMLList(val string) []string {
	inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(val), "["), "]")
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := trimYAMLQuotes(strings.TrimSpace(p))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func stripYAMLComment(line string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
		} else if ch == '#' && !inSingle && !inDouble {
			return line[:i]
		}
	}
	return line
}

func countLeadingSpaces(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			n++
		} else {
			break
		}
	}
	return n
}

func trimYAMLQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
