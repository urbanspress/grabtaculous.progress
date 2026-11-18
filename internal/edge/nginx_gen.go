package edge

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"grabtaculous/internal/safepath"
)

var (
	reSafeHostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	reSafeUpstream = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

// NginxConfigOptions configures production NGINX 1.30.6 Stable reverse-proxy generation.
type NginxConfigOptions struct {
	Domain          string `json:"domain"`
	WPDomain        string `json:"wp_domain,omitempty"`
	WorkerProcesses int    `json:"worker_processes"`
	UpstreamName    string `json:"upstream_name,omitempty"`
	UpstreamHost    string `json:"upstream_host"`
	UpstreamPorts   []int  `json:"upstream_ports"`
	UploadAliasDir  string `json:"upload_alias_dir"`
	CertBaseDir     string `json:"cert_base_dir"`
	WelcomeRootDir  string `json:"welcome_root_dir,omitempty"`
	DefaultSSLCert  string `json:"default_ssl_cert,omitempty"`
	DefaultSSLKey   string `json:"default_ssl_key,omitempty"`
	WPWebRoot       string `json:"wp_web_root"`
	WPPHPFPMAdr     string `json:"wp_php_fpm_addr"`
	MediaRateAfter  string `json:"media_rate_after"`
	MediaLimitRate  string `json:"media_limit_rate"`
	AccessLogPath   string `json:"access_log_path"`
	ErrorLogPath    string `json:"error_log_path"`
	EnableReusePort bool   `json:"enable_reuse_port"`
	HTTPOnly        bool   `json:"http_only,omitempty"`
}

// DefaultNginxConfigOptions returns Bluehost Ashburn (vps-8c-16g) 6-worker production defaults.
// When domain is empty, "_", or "default", it selects domain-free default_server mode ("_").
func DefaultNginxConfigOptions(domain string) NginxConfigOptions {
	d := strings.TrimSpace(domain)
	if d == "" || d == "_" || strings.EqualFold(d, "default") {
		d = "_"
	}
	wpDomain := ""
	if d != "_" {
		wpDomain = "blog." + d
	}
	return NginxConfigOptions{
		Domain:          d,
		WPDomain:        wpDomain,
		WorkerProcesses: 8,
		UpstreamName:    "grisp_backend",
		UpstreamHost:    "127.0.0.1",
		UpstreamPorts:   []int{8765, 8766, 8767, 8768, 8769, 8770},
		UploadAliasDir:  "/var/lib/grisp/uploads/",
		CertBaseDir:     "/etc/letsencrypt/live",
		WelcomeRootDir:  "/var/www/grisp-welcome",
		DefaultSSLCert:  "/etc/nginx/ssl/grisp-default.crt",
		DefaultSSLKey:   "/etc/nginx/ssl/grisp-default.key",
		WPWebRoot:       "/var/www/wordpress",
		WPPHPFPMAdr:     "127.0.0.1:9000",
		MediaRateAfter:  "20m",
		MediaLimitRate:  "12m",
		AccessLogPath:   "/var/log/nginx/access.log",
		ErrorLogPath:    "/var/log/nginx/error.log",
		EnableReusePort: true,
	}
}

// ValidateNginxOptions ensures hostnames, loopback upstreams, and ports obey ADR-005 & safe syntax.
func ValidateNginxOptions(opts *NginxConfigOptions) error {
	if opts == nil {
		return safepath.NewError(safepath.ErrSecurityPolicy, "nginx options cannot be nil", "Provide valid NginxConfigOptions.")
	}
	opts.Domain = strings.TrimSpace(opts.Domain)
	if opts.Domain == "" || opts.Domain == "_" || strings.EqualFold(opts.Domain, "default") {
		opts.Domain = "_"
	} else if !reSafeHostname.MatchString(opts.Domain) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid domain %q", opts.Domain), "Provide a clean hostname without spaces or injection characters.")
	}
	if strings.TrimSpace(opts.WPDomain) != "" && !reSafeHostname.MatchString(strings.TrimSpace(opts.WPDomain)) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid WordPress domain %q", opts.WPDomain), "Provide a clean hostname.")
	}
	if opts.WorkerProcesses <= 0 {
		opts.WorkerProcesses = 8
	}
	opts.UpstreamName = strings.TrimSpace(opts.UpstreamName)
	if opts.UpstreamName == "" {
		opts.UpstreamName = "grisp_backend"
	} else if !reSafeUpstream.MatchString(opts.UpstreamName) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid upstream_name %q", opts.UpstreamName), "Use alphanumeric characters and underscores starting with a letter.")
	}
	host := strings.TrimSpace(opts.UpstreamHost)
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopback(host) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("upstream host %q must be loopback (127.0.0.1 or ::1) per ADR-005", host), "Bind Grisp workers strictly to loopback.")
	}
	opts.UpstreamHost = host
	if len(opts.UpstreamPorts) == 0 {
		opts.UpstreamPorts = []int{8765, 8766, 8767, 8768, 8769, 8770}
	}
	for _, p := range opts.UpstreamPorts {
		if p < 1 || p > 65535 {
			return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid upstream port %d", p), "Use TCP ports 1-65535.")
		}
	}
	if strings.TrimSpace(opts.UploadAliasDir) == "" {
		opts.UploadAliasDir = "/var/lib/grisp/uploads/"
	}
	if !strings.HasSuffix(opts.UploadAliasDir, "/") {
		opts.UploadAliasDir += "/"
	}
	if strings.ContainsAny(opts.UploadAliasDir, ";{}\r\n") {
		return safepath.NewError(safepath.ErrSecurityPolicy, "invalid upload_alias_dir", "Remove NGINX control characters.")
	}
	if strings.TrimSpace(opts.CertBaseDir) == "" {
		opts.CertBaseDir = "/etc/letsencrypt/live"
	}
	opts.CertBaseDir = strings.TrimRight(strings.TrimSpace(opts.CertBaseDir), "/")
	if strings.ContainsAny(opts.CertBaseDir, ";{}\r\n") || strings.Contains(opts.CertBaseDir, "://") {
		return safepath.NewError(safepath.ErrSecurityPolicy, "invalid cert_base_dir", "Use a clean local filesystem path such as /etc/letsencrypt/live.")
	}
	if strings.TrimSpace(opts.WelcomeRootDir) == "" {
		opts.WelcomeRootDir = "/var/www/grisp-welcome"
	}
	opts.WelcomeRootDir = strings.TrimRight(strings.TrimSpace(opts.WelcomeRootDir), "/")
	if strings.ContainsAny(opts.WelcomeRootDir, ";{}\r\n") || strings.Contains(opts.WelcomeRootDir, "://") {
		return safepath.NewError(safepath.ErrSecurityPolicy, "invalid welcome_root_dir", "Use a clean local path such as /var/www/grisp-welcome.")
	}
	if strings.TrimSpace(opts.DefaultSSLCert) == "" {
		opts.DefaultSSLCert = "/etc/nginx/ssl/grisp-default.crt"
	}
	if strings.ContainsAny(opts.DefaultSSLCert, ";{}\r\n") || strings.Contains(opts.DefaultSSLCert, "://") {
		return safepath.NewError(safepath.ErrSecurityPolicy, "invalid default_ssl_cert", "Use a clean local certificate path.")
	}
	if strings.TrimSpace(opts.DefaultSSLKey) == "" {
		opts.DefaultSSLKey = "/etc/nginx/ssl/grisp-default.key"
	}
	if strings.ContainsAny(opts.DefaultSSLKey, ";{}\r\n") || strings.Contains(opts.DefaultSSLKey, "://") {
		return safepath.NewError(safepath.ErrSecurityPolicy, "invalid default_ssl_key", "Use a clean local key path.")
	}
	if strings.TrimSpace(opts.WPWebRoot) == "" {
		opts.WPWebRoot = "/var/www/wordpress"
	}
	if strings.TrimSpace(opts.WPPHPFPMAdr) == "" {
		opts.WPPHPFPMAdr = "127.0.0.1:9000"
	}
	if strings.TrimSpace(opts.MediaRateAfter) == "" {
		opts.MediaRateAfter = "20m"
	}
	if strings.TrimSpace(opts.MediaLimitRate) == "" {
		opts.MediaLimitRate = "12m"
	}
	if strings.TrimSpace(opts.AccessLogPath) == "" {
		opts.AccessLogPath = "/var/log/nginx/access.log"
	}
	if strings.TrimSpace(opts.ErrorLogPath) == "" {
		opts.ErrorLogPath = "/var/log/nginx/error.log"
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// RenderQuicSysctlConf returns /etc/sysctl.d/99-nginx-quic.conf for HTTP/3 QUIC UDP buffers.
func RenderQuicSysctlConf() []byte {
	return []byte(`# Grabtaculous v1.1.0 — NGINX 1.30.6 HTTP/3 (QUIC) & Socket Queue Tuning
net.core.rmem_max = 7500000
net.core.wmem_max = 7500000
net.core.somaxconn = 8192
`)
}

// RenderMainNginxConf emits the tuned official nginx.org 1.30.6 /etc/nginx/nginx.conf.
func RenderMainNginxConf(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("# Official nginx.org NGINX 1.30.6 (Stable) Main Configuration\n")
	b.WriteString("# Generated by Grabtaculous v1.1.0 (ADR-005 / GRAB v1.1.0 --trusted-proxy ready)\n\n")
	b.WriteString("user nginx;\n")
	fmt.Fprintf(&b, "worker_processes %d;\n", opts.WorkerProcesses)
	b.WriteString("worker_cpu_affinity auto;\n")
	b.WriteString("worker_rlimit_nofile 65535;\n")
	fmt.Fprintf(&b, "error_log %s warn;\n", opts.ErrorLogPath)
	b.WriteString("pid /run/nginx.pid;\n\n")

	b.WriteString("events {\n")
	b.WriteString("    worker_connections 8192;\n")
	b.WriteString("    multi_accept on;\n")
	b.WriteString("    use epoll;\n")
	b.WriteString("}\n\n")

	b.WriteString("http {\n")
	b.WriteString("    include /etc/nginx/mime.types;\n")
	b.WriteString("    default_type application/octet-stream;\n\n")

	b.WriteString("    log_format main '$remote_addr - $remote_user [$time_local] \"$request\" '\n")
	b.WriteString("                    '$status $body_bytes_sent \"$http_referer\" '\n")
	b.WriteString("                    '\"$http_user_agent\" \"$http_x_forwarded_for\" rt=$request_time';\n")
	fmt.Fprintf(&b, "    access_log %s main buffer=64k flush=5s;\n\n", opts.AccessLogPath)

	b.WriteString("    sendfile on;\n")
	b.WriteString("    sendfile_max_chunk 2m;\n")
	b.WriteString("    aio threads;\n")
	b.WriteString("    directio 8m;\n")
	b.WriteString("    tcp_nopush on;\n")
	b.WriteString("    tcp_nodelay on;\n")
	b.WriteString("    keepalive_timeout 65;\n")
	b.WriteString("    types_hash_max_size 2048;\n")
	b.WriteString("    server_tokens off;\n")
	b.WriteString("    client_max_body_size 1m;\n\n")

	b.WriteString("    open_file_cache max=10000 inactive=60s;\n")
	b.WriteString("    open_file_cache_valid 30s;\n")
	b.WriteString("    open_file_cache_min_uses 2;\n")
	b.WriteString("    open_file_cache_errors on;\n\n")

	b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
	b.WriteString("    ssl_prefer_server_ciphers off;\n")
	b.WriteString("    ssl_session_cache shared:SSL:20m;\n")
	b.WriteString("    ssl_session_timeout 1d;\n")
	b.WriteString("    ssl_session_tickets off;\n\n")

	b.WriteString("    # Selective edge Gzip for text/CSS/JSON/JSON-LD/JS/XML; strictly excludes pre-compressed binaries\n")
	b.WriteString("    # (.epub, .pdf, .m4b, .mp3, .webp, .png, .woff2).\n")
	b.WriteString("    gzip on;\n")
	b.WriteString("    gzip_vary on;\n")
	b.WriteString("    gzip_proxied any;\n")
	b.WriteString("    gzip_comp_level 5;\n")
	b.WriteString("    gzip_min_length 256;\n")
	b.WriteString("    gzip_buffers 16 8k;\n")
	b.WriteString("    gzip_types text/plain text/css application/json application/ld+json application/javascript text/javascript application/xml text/xml image/svg+xml;\n\n")

	b.WriteString("    # Outer socket flood shields (HTTP 429)\n")
	b.WriteString("    limit_req_zone $binary_remote_addr zone=auth_flood:10m rate=10r/s;\n")
	b.WriteString("    limit_req_zone $binary_remote_addr zone=media_flood:10m rate=5r/s;\n")
	b.WriteString("    limit_req_zone $binary_remote_addr zone=wp_login_flood:10m rate=2r/s;\n")
	b.WriteString("    limit_conn_zone $binary_remote_addr zone=media_conn:10m;\n")
	b.WriteString("    limit_req_status 429;\n")
	b.WriteString("    limit_conn_status 429;\n\n")

	b.WriteString("    map $http_upgrade $connection_upgrade {\n")
	b.WriteString("        default upgrade;\n")
	b.WriteString("        ''      close;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    include /etc/nginx/conf.d/*.conf;\n")
	b.WriteString("    include /etc/nginx/sites-enabled/*.conf;\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// RenderGrispSiteConf emits the primary NGINX 1.30.6 HTTP/3 + X-Accel-Redirect site config for Grisp workers.
// When opts.Domain == "_" (or empty), it emits fail-closed default_server catch-all blocks (return 444 on
// port 80 and ssl_reject_handshake on on port 443) so unconfigured public domains never render the Grisp
// Welcome page, while restricting /var/www/grisp-welcome/ strictly to private loopback (localhost / 127.0.0.1).
func RenderGrispSiteConf(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	reuseSuffix := ""
	if opts.EnableReusePort {
		reuseSuffix = " reuseport"
	}
	isDefaultMode := opts.Domain == "_"

	var b strings.Builder
	b.WriteString("# Primary Grisp Multi-Worker Site Configuration (NGINX 1.30.6 Stable + HTTP/3 + X-Accel-Redirect)\n")
	b.WriteString("# Generated by Grabtaculous v1.1.0 (ADR-005 / GRAB v1.1.0 --trusted-proxy 172.16.0.0/12,127.0.0.1)\n\n")

	fmt.Fprintf(&b, "upstream %s {\n", opts.UpstreamName)
	b.WriteString("    least_conn;\n")
	for _, p := range opts.UpstreamPorts {
		fmt.Fprintf(&b, "    server %s:%d max_fails=2 fail_timeout=5s;\n", opts.UpstreamHost, p)
	}
	b.WriteString("    keepalive 192;\n")
	b.WriteString("    keepalive_requests 10000;\n")
	b.WriteString("    keepalive_timeout 60s;\n")
	b.WriteString("}\n\n")

	if isDefaultMode {
		b.WriteString("# Catch-all HTTP (Port 80): drop unconfigured public domains and direct IP requests immediately\n")
		b.WriteString("server {\n")
		b.WriteString("    listen 80 default_server;\n")
		b.WriteString("    listen [::]:80 default_server;\n")
		b.WriteString("    server_name _;\n")
		b.WriteString("    return 444;\n")
		b.WriteString("}\n\n")

		b.WriteString("# Catch-all HTTPS (Port 443): reject TLS handshakes for any unconfigured domain or IP\n")
		b.WriteString("server {\n")
		b.WriteString("    listen 443 ssl default_server;\n")
		b.WriteString("    listen [::]:443 ssl default_server;\n")
		b.WriteString("    server_name _;\n")
		b.WriteString("    ssl_reject_handshake on;\n")
		b.WriteString("}\n\n")

		b.WriteString("# Private Loopback Grisp Welcome & Readiness Page (SSH tunnel / localhost only)\n")
		b.WriteString("server {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    listen [::]:80;\n")
		b.WriteString("    server_name localhost 127.0.0.1;\n\n")
		b.WriteString("    allow 127.0.0.1;\n")
		b.WriteString("    allow ::1;\n")
		b.WriteString("    deny all;\n\n")
		fmt.Fprintf(&b, "    root %s;\n", opts.WelcomeRootDir)
		b.WriteString("    index index.html;\n\n")
		b.WriteString("    location = / {\n")
		b.WriteString("        try_files /index.html =404;\n")
		b.WriteString("    }\n\n")
		b.WriteString("    location = /index.html {\n")
		b.WriteString("        add_header Cache-Control \"no-cache\" always;\n")
		b.WriteString("    }\n\n")
		b.WriteString("    location ^~ /grex/ {\n")
		b.WriteString("        try_files $uri =404;\n")
		b.WriteString("        add_header Cache-Control \"public, max-age=31536000, immutable\" always;\n")
		b.WriteString("    }\n\n")
		b.WriteString("    location ~ ^/_(health|ready)$ {\n")
		b.WriteString("        allow 127.0.0.1;\n")
		b.WriteString("        allow ::1;\n")
		b.WriteString("        deny all;\n")
		fmt.Fprintf(&b, "        proxy_pass http://%s;\n", opts.UpstreamName)
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Connection \"\";\n")
		b.WriteString("    }\n\n")
		b.WriteString("    location / {\n")
		b.WriteString("        try_files $uri $uri/ /index.html;\n")
		b.WriteString("    }\n")
		b.WriteString("}\n")
		return []byte(b.String()), nil
	}

	serverName := opts.Domain
	if !strings.EqualFold(opts.Domain, "localhost") && !strings.HasPrefix(strings.ToLower(opts.Domain), "www.") {
		serverName = fmt.Sprintf("%s www.%s", opts.Domain, opts.Domain)
	}
	if opts.HTTPOnly {
		b.WriteString("server {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    listen [::]:80;\n")
		fmt.Fprintf(&b, "    server_name %s;\n\n", serverName)
		b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
		b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n\n")
		b.WriteString("    location ^~ /.well-known/acme-challenge/ {\n")
		b.WriteString("        root /var/www/certbot;\n")
		b.WriteString("    }\n\n")
		writeGrispProxyLocations(&b, opts)
		b.WriteString("}\n")
		return []byte(b.String()), nil
	}

	certFullchain := fmt.Sprintf("%s/%s/fullchain.pem", opts.CertBaseDir, opts.Domain)
	certPrivkey := fmt.Sprintf("%s/%s/privkey.pem", opts.CertBaseDir, opts.Domain)

	b.WriteString("server {\n")
	b.WriteString("    listen 80;\n")
	b.WriteString("    listen [::]:80;\n")
	fmt.Fprintf(&b, "    server_name %s;\n", serverName)
	b.WriteString("    location /.well-known/acme-challenge/ {\n")
	b.WriteString("        root /var/www/certbot;\n")
	b.WriteString("    }\n")
	b.WriteString("    location / {\n")
	b.WriteString("        return 301 https://$host$request_uri;\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	fmt.Fprintf(&b, "    listen 443 quic%s;\n", reuseSuffix)
	fmt.Fprintf(&b, "    listen [::]:443 quic%s;\n", reuseSuffix)
	b.WriteString("    listen 443 ssl;\n")
	b.WriteString("    listen [::]:443 ssl;\n")
	b.WriteString("    http2 on;\n")
	fmt.Fprintf(&b, "    server_name %s;\n\n", serverName)

	fmt.Fprintf(&b, "    ssl_certificate %s;\n", certFullchain)
	fmt.Fprintf(&b, "    ssl_certificate_key %s;\n", certPrivkey)
	b.WriteString("    quic_retry on;\n")
	b.WriteString("    quic_gso on;\n")
	b.WriteString("    ssl_early_data off;\n\n")

	b.WriteString("    add_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	b.WriteString("    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;\n")
	b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n\n")

	writeGrispProxyLocations(&b, opts)
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeGrispProxyLocations(b *strings.Builder, opts NginxConfigOptions) {
	// 1. Default dynamic route proxy (RAM-only buffering, zero temp disk writes)
	b.WriteString("    location / {\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(b, "        ")
	b.WriteString("        proxy_buffer_size 16k;\n")
	b.WriteString("        proxy_buffers 16 32k;\n")
	b.WriteString("        proxy_busy_buffers_size 64k;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("        proxy_next_upstream error timeout http_502 http_503 http_504;\n")
	b.WriteString("    }\n\n")

	// 2. Auth brute-force shield
	b.WriteString("    location ^~ /api/auth/ {\n")
	b.WriteString("        limit_req zone=auth_flood burst=20 nodelay;\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(b, "        ")
	b.WriteString("        proxy_buffer_size 16k;\n")
	b.WriteString("        proxy_buffers 16 32k;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("    }\n\n")

	// 3. CMS admin media upload (11m body cap, zero request buffering)
	b.WriteString("    location = /api/admin/media/upload {\n")
	b.WriteString("        client_max_body_size 11m;\n")
	b.WriteString("        proxy_request_buffering off;\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(b, "        ")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("    }\n\n")

	// 4. Raw-byte HMAC webhooks (/api/webhooks/ & GrabLab /api/commerce/webhooks/)
	for _, webhookPrefix := range []string{"/api/webhooks/", "/api/commerce/webhooks/"} {
		fmt.Fprintf(b, "    location ^~ %s {\n", webhookPrefix)
		b.WriteString("        client_max_body_size 2m;\n")
		b.WriteString("        proxy_next_upstream off;\n")
		b.WriteString("        proxy_request_buffering off;\n")
		fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Connection \"\";\n")
		writeTrustedProxyHeaders(b, "        ")
		b.WriteString("    }\n\n")
	}

	// 5. Public media & audiobook download routes (rate shield + unbuffered streaming)
	b.WriteString("    location ^~ /media/ {\n")
	b.WriteString("        limit_req zone=media_flood burst=15 nodelay;\n")
	b.WriteString("        limit_conn media_conn 10;\n")
	b.WriteString("        gzip off;\n")
	b.WriteString("        proxy_buffering off;\n")
	b.WriteString("        proxy_request_buffering off;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	fmt.Fprintf(b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(b, "        limit_rate %s;\n", opts.MediaLimitRate)
	b.WriteString("        proxy_connect_timeout 10s;\n")
	b.WriteString("        proxy_send_timeout 600s;\n")
	b.WriteString("        proxy_read_timeout 600s;\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(b, "        ")
	b.WriteString("    }\n\n")

	// 6. Zero-copy internal NVMe X-Accel-Redirect location
	b.WriteString("    location ^~ /_protected_media/ {\n")
	b.WriteString("        internal;\n")
	fmt.Fprintf(b, "        alias %s;\n", opts.UploadAliasDir)
	b.WriteString("        sendfile on;\n")
	b.WriteString("        aio threads;\n")
	b.WriteString("        directio 8m;\n")
	b.WriteString("        output_buffers 2 1m;\n")
	b.WriteString("        gzip off;\n")
	b.WriteString("        limit_conn media_conn 10;\n")
	fmt.Fprintf(b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(b, "        limit_rate %s;\n", opts.MediaLimitRate)
	b.WriteString("        add_header X-Content-Type-Options \"nosniff\" always;\n")
	b.WriteString("    }\n\n")

	// 7. Zero-temp-disk internal Backblaze B2 S3-compatible X-Accel-Redirect location
	b.WriteString("    location ~ ^/_protected_b2/([^/]+)/(.+)$ {\n")
	b.WriteString("        internal;\n")
	b.WriteString("        resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;\n")
	b.WriteString("        set $b2_host $1;\n")
	b.WriteString("        set $b2_path $2;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Host $b2_host;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	b.WriteString("        proxy_hide_header x-amz-id-2;\n")
	b.WriteString("        proxy_hide_header x-amz-request-id;\n")
	b.WriteString("        proxy_hide_header x-bz-file-id;\n")
	b.WriteString("        proxy_hide_header x-bz-file-name;\n")
	b.WriteString("        proxy_hide_header x-bz-content-sha1;\n")
	b.WriteString("        proxy_ssl_server_name on;\n")
	b.WriteString("        proxy_buffering off;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("        gzip off;\n")
	b.WriteString("        limit_conn media_conn 10;\n")
	fmt.Fprintf(b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(b, "        limit_rate %s;\n", opts.MediaLimitRate)
	b.WriteString("        proxy_pass https://$b2_host/$b2_path$is_args$args;\n")
	b.WriteString("    }\n\n")

	// 8. WebSocket channels (use sockets Origin + Connection upgrade passthrough)
	b.WriteString("    location ^~ /ws/ {\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
	b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
	b.WriteString("        proxy_set_header Origin $http_origin;\n")
	writeTrustedProxyHeaders(b, "        ")
	b.WriteString("        proxy_read_timeout 60s;\n")
	b.WriteString("        proxy_send_timeout 60s;\n")
	b.WriteString("        proxy_next_upstream off;\n")
	b.WriteString("    }\n\n")

	// 9. Loopback-only health & readiness endpoints
	b.WriteString("    location ~ ^/_(health|ready)$ {\n")
	b.WriteString("        allow 127.0.0.1;\n")
	b.WriteString("        allow ::1;\n")
	b.WriteString("        deny all;\n")
	fmt.Fprintf(b, "        proxy_pass http://%s;\n", opts.UpstreamName)
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	b.WriteString("    }\n")
}

// RenderGrispWelcomeHTML emits the domain-free Next.js-style Grisp Default Welcome & Readiness Page
// built with zero wrapper <div> elements and powered by Grex JS v1.0.0.
func RenderGrispWelcomeHTML(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	workerCount := len(opts.UpstreamPorts)
	portRange := fmt.Sprintf("%s:%d", opts.UpstreamHost, opts.UpstreamPorts[0])
	if workerCount > 1 {
		portRange = fmt.Sprintf("%s:%d-%d", opts.UpstreamHost, opts.UpstreamPorts[0], opts.UpstreamPorts[workerCount-1])
	}
	var portItems strings.Builder
	for idx, p := range opts.UpstreamPorts {
		fmt.Fprintf(&portItems, "        <li><strong>grisp-worker-%d</strong> &mdash; <code>%s:%d</code> <span>/_ready</span></li>\n", idx+1, opts.UpstreamHost, p)
	}

	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Grisp Application Server — Edge Ready</title>
  <link rel="stylesheet" href="/grex/tokens.css">
  <link rel="stylesheet" href="/grex/welcome.css">
  <script type="module" src="/grex/welcome-grid.js"></script>
</head>
<body>
  <canvas id="grex-welcome-canvas" aria-hidden="true"></canvas>
  <main class="grisp-shell" data-grex-welcome="ready">
    <header class="grisp-hero">
      <p class="grisp-eyebrow" id="grisp-status-pill">GRISP EDGE &bull; HTTP/3 + QUIC READY</p>
      <h1>Welcome to Grisp.</h1>
      <p class="grisp-lead">Your domain-free NGINX 1.30.x HTTP/3 reverse proxy and loopback Grisp worker pool are online, verified, and ready for your first site deployment.</p>
      <nav class="grisp-actions" aria-label="Quickstart commands">
        <button type="button" class="grisp-btn" data-copy-cmd="grabman init">Copy <code>grabman init</code></button>
        <button type="button" class="grisp-btn grisp-btn-secondary" data-copy-cmd="grab serve --allow-scripts --static public src/app.grab">Copy <code>grab serve</code></button>
      </nav>
      <p class="grisp-copy-feedback" id="grisp-copy-feedback" role="status" aria-live="polite">Powered by Grex JS v1.0.0 &bull; Zero wrapper &lt;div&gt; architecture</p>
    </header>

    <section class="grisp-grid" aria-label="Edge and Runtime Readiness">
      <article class="grisp-card">
        <h2>Edge Transport</h2>
        <p><strong>NGINX 1.30.x Stable</strong></p>
        <ul>
          <li>HTTP/3 (QUIC) + HTTP/2 TLSv1.3 active</li>
          <li>Loopback-only welcome (<code>localhost</code>) &bull; Catch-all <code>default_server</code> drops unknown hosts (<code>444</code>)</li>
          <li>Zero-copy NVMe &amp; B2 <code>X-Accel-Redirect</code> routes armed</li>
        </ul>
      </article>

      <article class="grisp-card">
        <h2>Grisp Worker Pool</h2>
        <p><strong id="grisp-worker-count">%d Loopback Workers Configured</strong></p>
        <p><code>%s</code> (private <code>127.0.0.1</code> only)</p>
        <ul class="grisp-worker-list">
%s        </ul>
      </article>

      <article class="grisp-card">
        <h2>Next Steps</h2>
        <ol class="grisp-steps">
          <li>Scaffold a new project: <code>grabman init</code> (or <code>grab init</code>)</li>
          <li>Verify environment &amp; contracts: <code>grabman doctor</code></li>
          <li>Serve locally or attach a domain config: <code>grab serve</code></li>
          <li>Reload edge routing: <code>sudo nginx -t &amp;&amp; sudo systemctl reload nginx</code></li>
        </ol>
      </article>
    </section>

    <footer class="grisp-footer">
      <p>GRAB Ecosystem v1.1.0 &bull; Grisp Application Server &bull; Grex JS v1.0.0 Animated Canvas</p>
    </footer>
  </main>
</body>
</html>
`, workerCount, portRange, portItems.String())
	return []byte(page), nil
}

// RenderGrispWelcomeCSS emits externalized styles for the zero-<div> Grisp Welcome Page.
func RenderGrispWelcomeCSS() []byte {
	return []byte(`:root {
  --grisp-bg: #020617;
  --grisp-surface: rgba(15, 23, 42, 0.84);
  --grisp-border: rgba(52, 211, 153, 0.28);
  --grisp-text: #f8fafc;
  --grisp-muted: #cbd5e1;
  --grisp-accent: #10b981;
  --grisp-accent-soft: rgba(16, 185, 129, 0.14);
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  min-height: 100vh;
  background: radial-gradient(circle at top right, #064e3b 0%, var(--grisp-bg) 56%);
  color: var(--grisp-text);
  font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  line-height: 1.6;
  overflow-x: hidden;
}

#grex-welcome-canvas {
  position: fixed;
  inset: 0;
  width: 100%;
  height: 100%;
  z-index: 0;
  pointer-events: none;
}

.grisp-shell {
  position: relative;
  z-index: 1;
  max-width: 68rem;
  margin: 0 auto;
  padding: 4rem 1.5rem 3rem;
  display: grid;
  gap: 2.25rem;
}

.grisp-hero {
  background: var(--grisp-surface);
  border: 1px solid var(--grisp-border);
  border-radius: 1rem;
  padding: 2.5rem;
  backdrop-filter: blur(10px);
}

.grisp-eyebrow {
  display: inline-block;
  margin: 0 0 1rem;
  padding: 0.25rem 0.75rem;
  border-radius: 999px;
  background: var(--grisp-accent-soft);
  border: 1px solid var(--grisp-border);
  color: #6ee7b7;
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.08em;
}

h1 {
  margin: 0 0 0.75rem;
  font-size: clamp(2.1rem, 4.5vw, 3.25rem);
  letter-spacing: -0.03em;
  line-height: 1.1;
}

.grisp-lead {
  margin: 0 0 1.75rem;
  max-width: 46rem;
  color: var(--grisp-muted);
  font-size: 1.08rem;
}

.grisp-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
}

.grisp-btn {
  appearance: none;
  border: 1px solid var(--grisp-accent);
  background: var(--grisp-accent);
  color: #022c22;
  font-weight: 700;
  font-size: 0.95rem;
  padding: 0.7rem 1.15rem;
  border-radius: 0.65rem;
  cursor: pointer;
}

.grisp-btn-secondary {
  background: rgba(15, 23, 42, 0.9);
  color: #ecfdf5;
}

.grisp-btn:focus-visible {
  outline: 3px solid #6ee7b7;
  outline-offset: 2px;
}

.grisp-copy-feedback {
  margin: 1rem 0 0;
  font-size: 0.9rem;
  color: #a7f3d0;
}

.grisp-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(17.5rem, 1fr));
  gap: 1.25rem;
}

.grisp-card {
  background: var(--grisp-surface);
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 0.9rem;
  padding: 1.5rem;
  backdrop-filter: blur(8px);
}

.grisp-card h2 {
  margin: 0 0 0.75rem;
  font-size: 1.12rem;
  color: #6ee7b7;
}

.grisp-card ul,
.grisp-card ol {
  margin: 0.5rem 0 0;
  padding-left: 1.25rem;
  color: var(--grisp-muted);
}

.grisp-card li {
  margin-bottom: 0.4rem;
}

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.88em;
  background: rgba(2, 6, 23, 0.7);
  padding: 0.14rem 0.38rem;
  border-radius: 0.3rem;
  color: #a7f3d0;
}

.grisp-footer {
  text-align: center;
  color: var(--grisp-muted);
  font-size: 0.88rem;
}
`)
}

// RenderGrispWelcomeGridJS emits the lightweight Grex JS v1.0.0 animated particle/grid canvas script.
func RenderGrispWelcomeGridJS() []byte {
	return []byte(`import { signal, effect, setSafeText } from "/grex/grex.js";

const feedbackNode = document.getElementById("grisp-copy-feedback");
const copySignal = signal("Powered by Grex JS v1.0.0 • Zero wrapper <div> architecture");

effect(() => {
  if (feedbackNode) {
    setSafeText(feedbackNode, copySignal());
  }
});

for (const btn of document.querySelectorAll("[data-copy-cmd]")) {
  btn.addEventListener("click", async () => {
    const cmd = btn.getAttribute("data-copy-cmd") || "";
    if (!cmd) return;
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(cmd);
      }
      copySignal.set("Copied command: " + cmd);
    } catch {
      copySignal.set("Run in terminal: " + cmd);
    }
  });
}

const canvas = document.getElementById("grex-welcome-canvas");
if (canvas && canvas.getContext) {
  const ctx = canvas.getContext("2d");
  const reducedMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  let width = 0;
  let height = 0;
  const particles = [];
  const PARTICLE_COUNT = 42;

  function resize() {
    width = canvas.width = window.innerWidth;
    height = canvas.height = window.innerHeight;
  }
  resize();
  window.addEventListener("resize", resize);

  for (let i = 0; i < PARTICLE_COUNT; i++) {
    particles.push({
      x: Math.random() * (width || 1280),
      y: Math.random() * (height || 720),
      vx: (Math.random() - 0.5) * 0.45,
      vy: (Math.random() - 0.5) * 0.45,
      r: 1.5 + Math.random() * 1.8,
    });
  }

  function drawFrame() {
    ctx.clearRect(0, 0, width, height);

    ctx.strokeStyle = "rgba(16, 185, 129, 0.08)";
    ctx.lineWidth = 1;
    const step = 48;
    for (let x = 0; x < width; x += step) {
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, height);
      ctx.stroke();
    }
    for (let y = 0; y < height; y += step) {
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(width, y);
      ctx.stroke();
    }

    for (let i = 0; i < particles.length; i++) {
      const p = particles[i];
      if (!reducedMotion) {
        p.x += p.vx;
        p.y += p.vy;
        if (p.x < 0 || p.x > width) p.vx *= -1;
        if (p.y < 0 || p.y > height) p.vy *= -1;
      }

      ctx.fillStyle = "rgba(52, 211, 153, 0.65)";
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
      ctx.fill();

      for (let j = i + 1; j < particles.length; j++) {
        const q = particles[j];
        const dx = p.x - q.x;
        const dy = p.y - q.y;
        const dist = Math.hypot(dx, dy);
        if (dist < 140) {
          ctx.strokeStyle = "rgba(16, 185, 129, " + (0.22 * (1 - dist / 140)).toFixed(3) + ")";
          ctx.beginPath();
          ctx.moveTo(p.x, p.y);
          ctx.lineTo(q.x, q.y);
          ctx.stroke();
        }
      }
    }

    if (!reducedMotion) {
      window.requestAnimationFrame(drawFrame);
    }
  }

  drawFrame();
}
`)
}

// RenderWordPressFPMConf emits an isolated NGINX 1.30.6 PHP-FPM companion vhost (no duplicate reuseport, no Debian snippets).
func RenderWordPressFPMConf(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	wpDomain := strings.TrimSpace(opts.WPDomain)
	if wpDomain == "" {
		if opts.Domain == "_" {
			return nil, safepath.NewError(safepath.ErrSecurityPolicy, "wordpress companion domain is required when grisp site is in domain-free default mode", "Pass --wp-domain <domain> or a registered --domain.")
		}
		wpDomain = "blog." + opts.Domain
	}
	certFullchain := fmt.Sprintf("%s/%s/fullchain.pem", opts.CertBaseDir, wpDomain)
	certPrivkey := fmt.Sprintf("%s/%s/privkey.pem", opts.CertBaseDir, wpDomain)

	var b strings.Builder
	b.WriteString("# Isolated WordPress / PHP-FPM Companion Site Configuration (NGINX 1.30.6 Stable)\n")
	b.WriteString("# Note: Primary grisp.conf already declares the single :443 socket listener flag; omitted here so nginx -t passes.\n\n")

	if opts.HTTPOnly {
		b.WriteString("server {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    listen [::]:80;\n")
		fmt.Fprintf(&b, "    server_name %s;\n\n", wpDomain)
		fmt.Fprintf(&b, "    root %s;\n", opts.WPWebRoot)
		b.WriteString("    index index.php index.html;\n")
		b.WriteString("    client_max_body_size 64m;\n\n")
		b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
		b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n\n")
		b.WriteString("    location ^~ /.well-known/acme-challenge/ {\n")
		b.WriteString("        root /var/www/certbot;\n")
		b.WriteString("    }\n\n")
		writeWordPressFPMLocations(&b, opts)
		b.WriteString("}\n")
		return []byte(b.String()), nil
	}

	b.WriteString("server {\n")
	b.WriteString("    listen 80;\n")
	b.WriteString("    listen [::]:80;\n")
	fmt.Fprintf(&b, "    server_name %s;\n", wpDomain)
	b.WriteString("    location /.well-known/acme-challenge/ {\n")
	b.WriteString("        root /var/www/certbot;\n")
	b.WriteString("    }\n")
	b.WriteString("    location / {\n")
	b.WriteString("        return 301 https://$host$request_uri;\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	b.WriteString("    listen 443 quic;\n")
	b.WriteString("    listen [::]:443 quic;\n")
	b.WriteString("    listen 443 ssl;\n")
	b.WriteString("    listen [::]:443 ssl;\n")
	b.WriteString("    http2 on;\n")
	fmt.Fprintf(&b, "    server_name %s;\n\n", wpDomain)

	fmt.Fprintf(&b, "    root %s;\n", opts.WPWebRoot)
	b.WriteString("    index index.php index.html;\n")
	b.WriteString("    client_max_body_size 64m;\n\n")

	fmt.Fprintf(&b, "    ssl_certificate %s;\n", certFullchain)
	fmt.Fprintf(&b, "    ssl_certificate_key %s;\n", certPrivkey)
	b.WriteString("    quic_retry on;\n")
	b.WriteString("    quic_gso on;\n")
	b.WriteString("    ssl_early_data off;\n\n")

	b.WriteString("    add_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n\n")

	writeWordPressFPMLocations(&b, opts)
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeWordPressFPMLocations(b *strings.Builder, opts NginxConfigOptions) {
	b.WriteString("    location = /xmlrpc.php {\n")
	b.WriteString("        deny all;\n")
	b.WriteString("        return 403;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    location ~* /wp-config\\.php {\n")
	b.WriteString("        deny all;\n")
	b.WriteString("        return 403;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    location ~* ^/wp-content/uploads/.*\\.php$ {\n")
	b.WriteString("        deny all;\n")
	b.WriteString("        return 403;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    location = /wp-login.php {\n")
	b.WriteString("        limit_req zone=wp_login_flood burst=5 nodelay;\n")
	b.WriteString("        include /etc/nginx/fastcgi_params;\n")
	b.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	fmt.Fprintf(b, "        fastcgi_pass %s;\n", opts.WPPHPFPMAdr)
	b.WriteString("    }\n\n")

	b.WriteString("    location / {\n")
	b.WriteString("        try_files $uri $uri/ /index.php?$args;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    location ~ \\.php$ {\n")
	b.WriteString("        try_files $uri =404;\n")
	b.WriteString("        include /etc/nginx/fastcgi_params;\n")
	b.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	b.WriteString("        fastcgi_index index.php;\n")
	fmt.Fprintf(b, "        fastcgi_pass %s;\n", opts.WPPHPFPMAdr)
	b.WriteString("    }\n")
}

func writeTrustedProxyHeaders(b *strings.Builder, indent string) {
	b.WriteString(indent + "proxy_set_header Host $host;\n")
	b.WriteString(indent + "proxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-Host $host;\n")
}
