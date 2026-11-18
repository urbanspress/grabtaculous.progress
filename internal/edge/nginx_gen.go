package edge

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"grabtaculous/internal/safepath"
)

var reSafeHostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// NginxConfigOptions configures production NGINX 1.30.6 Stable reverse-proxy generation.
type NginxConfigOptions struct {
	Domain           string   `json:"domain"`
	WPDomain         string   `json:"wp_domain,omitempty"`
	WorkerProcesses  int      `json:"worker_processes"`
	UpstreamHost     string   `json:"upstream_host"`
	UpstreamPorts    []int    `json:"upstream_ports"`
	UploadAliasDir   string   `json:"upload_alias_dir"`
	CertBaseDir      string   `json:"cert_base_dir"`
	WPWebRoot        string   `json:"wp_web_root"`
	WPPHPFPMAdr      string   `json:"wp_php_fpm_addr"`
	MediaRateAfter   string   `json:"media_rate_after"`
	MediaLimitRate   string   `json:"media_limit_rate"`
	AccessLogPath    string   `json:"access_log_path"`
	ErrorLogPath     string   `json:"error_log_path"`
	EnableReusePort  bool     `json:"enable_reuse_port"`
}

// DefaultNginxConfigOptions returns Bluehost Ashburn (vps-8c-16g) 6-worker production defaults.
func DefaultNginxConfigOptions(domain string) NginxConfigOptions {
	d := strings.TrimSpace(domain)
	if d == "" {
		d = "bookstore.example.com"
	}
	return NginxConfigOptions{
		Domain:          d,
		WPDomain:        "blog." + d,
		WorkerProcesses: 8,
		UpstreamHost:    "127.0.0.1",
		UpstreamPorts:   []int{8765, 8766, 8767, 8768, 8769, 8770},
		UploadAliasDir:  "/var/lib/grisp/uploads/",
		CertBaseDir:     "/etc/letsencrypt/live",
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
	if opts.Domain == "" {
		opts.Domain = "localhost"
	}
	if !reSafeHostname.MatchString(opts.Domain) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid domain %q", opts.Domain), "Provide a clean hostname without spaces or injection characters.")
	}
	if strings.TrimSpace(opts.WPDomain) != "" && !reSafeHostname.MatchString(strings.TrimSpace(opts.WPDomain)) {
		return safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("invalid WordPress domain %q", opts.WPDomain), "Provide a clean hostname.")
	}
	if opts.WorkerProcesses <= 0 {
		opts.WorkerProcesses = 8
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
func RenderGrispSiteConf(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	reuseSuffix := ""
	if opts.EnableReusePort {
		reuseSuffix = " reuseport"
	}
	certFullchain := fmt.Sprintf("%s/%s/fullchain.pem", opts.CertBaseDir, opts.Domain)
	certPrivkey := fmt.Sprintf("%s/%s/privkey.pem", opts.CertBaseDir, opts.Domain)

	var b strings.Builder
	b.WriteString("# Primary Grisp Multi-Worker Site Configuration (NGINX 1.30.6 Stable + HTTP/3 + X-Accel-Redirect)\n")
	b.WriteString("# Generated by Grabtaculous v1.1.0 (ADR-005 / GRAB v1.1.0 --trusted-proxy 172.16.0.0/12,127.0.0.1)\n\n")

	b.WriteString("upstream grisp_backend {\n")
	b.WriteString("    least_conn;\n")
	for _, p := range opts.UpstreamPorts {
		fmt.Fprintf(&b, "    server %s:%d max_fails=2 fail_timeout=5s;\n", opts.UpstreamHost, p)
	}
	b.WriteString("    keepalive 192;\n")
	b.WriteString("    keepalive_requests 10000;\n")
	b.WriteString("    keepalive_timeout 60s;\n")
	b.WriteString("}\n\n")

	b.WriteString("server {\n")
	b.WriteString("    listen 80;\n")
	b.WriteString("    listen [::]:80;\n")
	fmt.Fprintf(&b, "    server_name %s;\n", opts.Domain)
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
	fmt.Fprintf(&b, "    server_name %s;\n\n", opts.Domain)

	fmt.Fprintf(&b, "    ssl_certificate %s;\n", certFullchain)
	fmt.Fprintf(&b, "    ssl_certificate_key %s;\n", certPrivkey)
	b.WriteString("    quic_retry on;\n")
	b.WriteString("    quic_gso on;\n")
	b.WriteString("    ssl_early_data off;\n\n")

	b.WriteString("    add_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	b.WriteString("    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;\n")
	b.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
	b.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n\n")

	// 1. Default dynamic route proxy (RAM-only buffering, zero temp disk writes)
	b.WriteString("    location / {\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(&b, "        ")
	b.WriteString("        proxy_buffer_size 16k;\n")
	b.WriteString("        proxy_buffers 16 32k;\n")
	b.WriteString("        proxy_busy_buffers_size 64k;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("        proxy_next_upstream error timeout http_502 http_503 http_504;\n")
	b.WriteString("    }\n\n")

	// 2. Auth brute-force shield
	b.WriteString("    location ^~ /api/auth/ {\n")
	b.WriteString("        limit_req zone=auth_flood burst=20 nodelay;\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(&b, "        ")
	b.WriteString("        proxy_buffer_size 16k;\n")
	b.WriteString("        proxy_buffers 16 32k;\n")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("    }\n\n")

	// 3. CMS admin media upload (11m body cap, zero request buffering)
	b.WriteString("    location = /api/admin/media/upload {\n")
	b.WriteString("        client_max_body_size 11m;\n")
	b.WriteString("        proxy_request_buffering off;\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(&b, "        ")
	b.WriteString("        proxy_max_temp_file_size 0;\n")
	b.WriteString("    }\n\n")

	// 4. Raw-byte HMAC webhooks (/api/webhooks/ & GrabLab /api/commerce/webhooks/)
	for _, webhookPrefix := range []string{"/api/webhooks/", "/api/commerce/webhooks/"} {
		fmt.Fprintf(&b, "    location ^~ %s {\n", webhookPrefix)
		b.WriteString("        client_max_body_size 2m;\n")
		b.WriteString("        proxy_next_upstream off;\n")
		b.WriteString("        proxy_request_buffering off;\n")
		b.WriteString("        proxy_pass http://grisp_backend;\n")
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Connection \"\";\n")
		writeTrustedProxyHeaders(&b, "        ")
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
	fmt.Fprintf(&b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(&b, "        limit_rate %s;\n", opts.MediaLimitRate)
	b.WriteString("        proxy_connect_timeout 10s;\n")
	b.WriteString("        proxy_send_timeout 600s;\n")
	b.WriteString("        proxy_read_timeout 600s;\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	writeTrustedProxyHeaders(&b, "        ")
	b.WriteString("    }\n\n")

	// 6. Zero-copy internal NVMe X-Accel-Redirect location
	b.WriteString("    location ^~ /_protected_media/ {\n")
	b.WriteString("        internal;\n")
	fmt.Fprintf(&b, "        alias %s;\n", opts.UploadAliasDir)
	b.WriteString("        sendfile on;\n")
	b.WriteString("        aio threads;\n")
	b.WriteString("        directio 8m;\n")
	b.WriteString("        output_buffers 2 1m;\n")
	b.WriteString("        gzip off;\n")
	b.WriteString("        limit_conn media_conn 10;\n")
	fmt.Fprintf(&b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(&b, "        limit_rate %s;\n", opts.MediaLimitRate)
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
	fmt.Fprintf(&b, "        limit_rate_after %s;\n", opts.MediaRateAfter)
	fmt.Fprintf(&b, "        limit_rate %s;\n", opts.MediaLimitRate)
	b.WriteString("        proxy_pass https://$b2_host/$b2_path$is_args$args;\n")
	b.WriteString("    }\n\n")

	// 8. WebSocket channels (use sockets Origin + Connection upgrade passthrough)
	b.WriteString("    location ^~ /ws/ {\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
	b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
	b.WriteString("        proxy_set_header Origin $http_origin;\n")
	writeTrustedProxyHeaders(&b, "        ")
	b.WriteString("        proxy_next_upstream off;\n")
	b.WriteString("    }\n\n")

	// 9. Loopback-only health & readiness endpoints
	b.WriteString("    location ~ ^/_(health|ready)$ {\n")
	b.WriteString("        allow 127.0.0.1;\n")
	b.WriteString("        allow ::1;\n")
	b.WriteString("        deny all;\n")
	b.WriteString("        proxy_pass http://grisp_backend;\n")
	b.WriteString("        proxy_http_version 1.1;\n")
	b.WriteString("        proxy_set_header Connection \"\";\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// RenderWordPressFPMConf emits an isolated NGINX 1.30.6 PHP-FPM companion vhost (no duplicate reuseport, no Debian snippets).
func RenderWordPressFPMConf(opts NginxConfigOptions) ([]byte, error) {
	if err := ValidateNginxOptions(&opts); err != nil {
		return nil, err
	}
	wpDomain := strings.TrimSpace(opts.WPDomain)
	if wpDomain == "" {
		wpDomain = "blog." + opts.Domain
	}
	certFullchain := fmt.Sprintf("%s/%s/fullchain.pem", opts.CertBaseDir, wpDomain)
	certPrivkey := fmt.Sprintf("%s/%s/privkey.pem", opts.CertBaseDir, wpDomain)

	var b strings.Builder
	b.WriteString("# Isolated WordPress / PHP-FPM Companion Site Configuration (NGINX 1.30.6 Stable)\n")
	b.WriteString("# Note: Primary grisp.conf already declares the single :443 socket listener flag; omitted here so nginx -t passes.\n\n")

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
	fmt.Fprintf(&b, "        fastcgi_pass %s;\n", opts.WPPHPFPMAdr)
	b.WriteString("    }\n\n")

	b.WriteString("    location / {\n")
	b.WriteString("        try_files $uri $uri/ /index.php?$args;\n")
	b.WriteString("    }\n\n")

	b.WriteString("    location ~ \\.php$ {\n")
	b.WriteString("        try_files $uri =404;\n")
	b.WriteString("        include /etc/nginx/fastcgi_params;\n")
	b.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	b.WriteString("        fastcgi_index index.php;\n")
	fmt.Fprintf(&b, "        fastcgi_pass %s;\n", opts.WPPHPFPMAdr)
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeTrustedProxyHeaders(b *strings.Builder, indent string) {
	b.WriteString(indent + "proxy_set_header Host $host;\n")
	b.WriteString(indent + "proxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString(indent + "proxy_set_header X-Forwarded-Host $host;\n")
}
