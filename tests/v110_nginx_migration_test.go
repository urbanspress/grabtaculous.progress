package tests

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grabtaculous/internal/compose"
	"grabtaculous/internal/container"
	"grabtaculous/internal/edge"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

func TestNginx1306BootstrapPlanAndLegacySanitization(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	nginxDir := filepath.Join(tmp, "etc", "nginx")
	if err := os.MkdirAll(filepath.Join(nginxDir, "sites-enabled"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyMain := `user www-data;
include /etc/nginx/modules-enabled/*.conf;
http {
  server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    include snippets/fastcgi-php.conf;
  }
}
`
	if err := os.WriteFile(filepath.Join(nginxDir, "nginx.conf"), []byte(legacyMain), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	mockRunner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		switch name {
		case "gpg":
			if len(args) > 0 && args[0] == "--show-keys" {
				return []byte("fpr:::::::::" + edge.NginxSigningKeyFingerprint + ":\n"), nil
			}
		case "nginx":
			if len(args) > 0 && args[0] == "-v" {
				return []byte("nginx version: nginx/1.30.6\n"), nil
			}
		}
		return []byte("ok"), nil
	}

	res, err := edge.ExecuteNginx130Bootstrap(ctx, edge.BootstrapOptions{
		NginxConfigDir: nginxDir,
		StateDir:       filepath.Join(tmp, "state"),
		KeyringPath:    filepath.Join(tmp, "keyring", "nginx.gpg"),
		AptListPath:    filepath.Join(tmp, "apt", "nginx.list"),
		AptPrefPath:    filepath.Join(tmp, "apt", "99nginx"),
		SysctlPath:     filepath.Join(tmp, "sysctl", "99-nginx-quic.conf"),
		DistroCodename: "noble",
		Runner:         mockRunner,
	})
	if err != nil {
		t.Fatalf("ExecuteNginx130Bootstrap failed: %v", err)
	}
	if res.SnapshotID == "" || res.NginxVersion != "nginx version: nginx/1.30.6" {
		t.Fatalf("unexpected bootstrap result: %+v", res)
	}

	// Verify sanitized nginx.conf stripped Debian-only modules-enabled, fastcgi-php.conf, and listen 443 ssl http2
	cleaned, err := os.ReadFile(filepath.Join(nginxDir, "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	cleanedStr := string(cleaned)
	if strings.Contains(cleanedStr, "include /etc/nginx/modules-enabled/") ||
		strings.Contains(cleanedStr, "snippets/fastcgi-php.conf") ||
		strings.Contains(cleanedStr, "listen 443 ssl http2;") {
		t.Fatalf("legacy Ubuntu 1.24 directives remained after sanitization:\n%s", cleanedStr)
	}
	if !strings.Contains(cleanedStr, "include /etc/nginx/fastcgi_params;") || !strings.Contains(cleanedStr, "http2 on;") {
		t.Fatalf("expected modernized fastcgi_params and http2 on; directives:\n%s", cleanedStr)
	}

	// Verify structured-argv zero-shell execution (never sh -c) & 1.24.x rejection
	for _, c := range calls {
		if len(c) > 0 && (c[0] == "sh" || c[0] == "bash") {
			t.Fatalf("forbidden shell invocation detected in bootstrapper: %v", c)
		}
	}
	if err := edge.VerifyNginxVersionMin("nginx version: nginx/1.24.0 (Ubuntu)", 1, 30, 1); safepath.ExtractCode(err) != safepath.ErrSecurityPolicy {
		t.Fatalf("expected GRABTAC-E401 rejecting stock nginx 1.24.0, got: %v", err)
	}

	// Verify sysctl & apt pin contents
	sysctlRaw, _ := os.ReadFile(filepath.Join(tmp, "sysctl", "99-nginx-quic.conf"))
	if !strings.Contains(string(sysctlRaw), "net.core.rmem_max = 7500000") || !strings.Contains(string(sysctlRaw), "net.core.somaxconn = 8192") {
		t.Fatalf("unexpected 99-nginx-quic.conf:\n%s", string(sysctlRaw))
	}
}

func TestNginx1306ProductionTemplatesXAccelAndWordPressParity(t *testing.T) {
	opts := edge.DefaultNginxConfigOptions("bookstore.example.com")
	opts.WPDomain = "blog.bookstore.example.com"

	mainConf, err := edge.RenderMainNginxConf(opts)
	if err != nil {
		t.Fatalf("RenderMainNginxConf failed: %v", err)
	}
	mainTxt := string(mainConf)
	for _, required := range []string{
		"worker_processes 8;",
		"worker_cpu_affinity auto;",
		"worker_rlimit_nofile 65535;",
		"worker_connections 8192;",
		"aio threads;",
		"directio 8m;",
		"sendfile_max_chunk 2m;",
		"gzip_comp_level 5;",
		"application/ld+json",
		"zone=auth_flood:10m rate=10r/s;",
		"zone=media_flood:10m rate=5r/s;",
		"zone=wp_login_flood:10m rate=2r/s;",
		"zone=media_conn:10m;",
		"limit_conn_status 429;",
		"include /etc/nginx/conf.d/*.conf;",
		"include /etc/nginx/sites-enabled/*.conf;",
	} {
		if !strings.Contains(mainTxt, required) {
			t.Fatalf("nginx.conf missing %q", required)
		}
	}
	if strings.Contains(mainTxt, "/etc/nginx/modules-enabled/") {
		t.Fatalf("nginx.conf must not include Ubuntu modules-enabled")
	}

	grispConf, err := edge.RenderGrispSiteConf(opts)
	if err != nil {
		t.Fatalf("RenderGrispSiteConf failed: %v", err)
	}
	grispTxt := string(grispConf)
	for _, required := range []string{
		"least_conn;",
		"server 127.0.0.1:8765 max_fails=2 fail_timeout=5s;",
		"server 127.0.0.1:8770 max_fails=2 fail_timeout=5s;",
		"keepalive 192;",
		"listen 443 quic reuseport;",
		"listen [::]:443 quic reuseport;",
		"http2 on;",
		"quic_retry on;",
		"quic_gso on;",
		"ssl_early_data off;",
		"add_header Alt-Svc 'h3=\":443\"; ma=86400' always;",
		"ssl_certificate /etc/letsencrypt/live/bookstore.example.com/fullchain.pem;",
		"proxy_set_header X-Forwarded-Host $host;",
		"location = /api/admin/media/upload",
		"client_max_body_size 11m;",
		"location ^~ /api/webhooks/",
		"location ^~ /api/commerce/webhooks/",
		"location ^~ /media/",
		"location ^~ /_protected_media/",
		"alias /var/lib/grisp/uploads/;",
		"directio 8m;",
		"output_buffers 2 1m;",
		"location ~ ^/_protected_b2/([^/]+)/(.+)$",
		"proxy_hide_header x-bz-file-id;",
		"proxy_hide_header x-bz-content-sha1;",
		"proxy_pass https://$b2_host/$b2_path$is_args$args;",
		"limit_rate_after 20m;",
		"limit_rate 12m;",
		"location ^~ /ws/",
		"proxy_set_header Origin $http_origin;",
		"proxy_read_timeout 60s;",
		"proxy_send_timeout 60s;",
		"location ~ ^/_(health|ready)$",
	} {
		if !strings.Contains(grispTxt, required) {
			t.Fatalf("grisp.conf missing %q", required)
		}
	}
	if strings.Contains(grispTxt, "ssl_stapling") || strings.Contains(grispTxt, "listen 443 ssl http2;") || strings.Contains(grispTxt, "/etc/letsencrypt/live/://") {
		t.Fatalf("grisp.conf contains deprecated or malformed SSL directives")
	}

	wpConf, err := edge.RenderWordPressFPMConf(opts)
	if err != nil {
		t.Fatalf("RenderWordPressFPMConf failed: %v", err)
	}
	wpTxt := string(wpConf)
	if strings.Contains(wpTxt, "reuseport") || strings.Contains(wpTxt, "snippets/fastcgi-php.conf") {
		t.Fatalf("wordpress-fpm.conf must omit reuseport and Debian snippets/fastcgi-php.conf:\n%s", wpTxt)
	}
	for _, required := range []string{
		"listen 443 quic;",
		"http2 on;",
		"client_max_body_size 64m;",
		"include /etc/nginx/fastcgi_params;",
		"fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;",
		"location = /xmlrpc.php",
		"location ~* /wp-config\\.php",
		"location ~* ^/wp-content/uploads/.*\\.php$",
		"limit_req zone=wp_login_flood burst=5 nodelay;",
	} {
		if !strings.Contains(wpTxt, required) {
			t.Fatalf("wordpress-fpm.conf missing %q", required)
		}
	}
}

func TestHardwareProfilesNVMeReserveGuardAndGrabmanDoctor(t *testing.T) {
	profiles := compose.ListProfiles()
	if len(profiles) != 5 {
		t.Fatalf("expected 5 hardware profiles, got %d", len(profiles))
	}

	for _, p := range profiles {
		t.Run(p.ID, func(t *testing.T) {
			yamlBytes, err := compose.RenderProfileCompose(p.ID, "bookstore.example.com")
			if err != nil {
				t.Fatalf("RenderProfileCompose(%s) failed: %v", p.ID, err)
			}
			if !strings.Contains(string(yamlBytes), "stop_grace_period: 35s") {
				t.Fatalf("profile %s missing stop_grace_period: 35s", p.ID)
			}
			if err := container.AuditComposeYAML("deploy/docker-compose.yml", string(yamlBytes), container.AuditOptions{RequireDigest: true}); err != nil {
				t.Fatalf("profile %s failed GRABTAC-E401 audit: %v", p.ID, err)
			}
			if issues := container.ScanNoPlaintextSecrets("deploy/docker-compose.yml", string(yamlBytes)); len(issues) > 0 {
				t.Fatalf("profile %s leaked secret/IP: %v", p.ID, issues)
			}
			pgConf, err := compose.RenderPostgresConf(p.ID)
			if err != nil || !strings.Contains(string(pgConf), p.PostgresSharedBuffers) {
				t.Fatalf("unexpected postgres.conf for %s: err=%v", p.ID, err)
			}
			reloadSh, err := compose.RenderReloadWorkersScript(p.ID)
			if err != nil || !strings.Contains(string(reloadSh), "--sighup") || !strings.Contains(string(reloadSh), "--recreate") || !strings.Contains(string(reloadSh), `"status":"ready"`) {
				t.Fatalf("unexpected reload-workers.sh for %s", p.ID)
			}
			backupSh, err := compose.RenderGrispBackupScript(p.ID)
			if err != nil || !strings.Contains(string(backupSh), "GRAB_B2_ENDPOINT") || !strings.Contains(string(backupSh), "MIN_FREE_KB") {
				t.Fatalf("unexpected grisp-backup.sh for %s", p.ID)
			}
		})
	}

	// Verify 15 GB NVMe Reserve Guard refuses < 15 GB free or > 92% used
	lowFree := func(string) (compose.DiskStats, error) {
		return compose.DiskStats{
			TotalBytes:     200 * 1024 * 1024 * 1024,
			AvailableBytes: 12 * 1024 * 1024 * 1024, // 12 GB < 15 GB
		}, nil
	}
	if _, err := compose.CheckDiskReserve(".", 0, lowFree); safepath.ExtractCode(err) != safepath.ErrSecurityPolicy {
		t.Fatalf("expected GRABTAC-E401 for < 15 GB NVMe reserve, got: %v", err)
	}
	highPct := func(string) (compose.DiskStats, error) {
		return compose.DiskStats{
			TotalBytes:     500 * 1024 * 1024 * 1024,
			AvailableBytes: 30 * 1024 * 1024 * 1024, // 30 GB free, but 94% used > 92%
		}, nil
	}
	if _, err := compose.CheckDiskReserve(".", 0, highPct); safepath.ExtractCode(err) != safepath.ErrSecurityPolicy {
		t.Fatalf("expected GRABTAC-E401 for > 92%% NVMe usage, got: %v", err)
	}

	// Verify snapshot.Create and rollout.RunPreflight enforce the 15 GB NVMe reserve guard
	ws := t.TempDir()
	if _, err := snapshot.Create(context.Background(), snapshot.CreateOptions{
		Workspace:  ws,
		DiskStatFn: lowFree,
	}); safepath.ExtractCode(err) != safepath.ErrSnapshotFailed {
		t.Fatalf("expected GRABTAC-E701 when snapshot.Create hits NVMe reserve, got: %v", err)
	}
	if _, err := rollout.RunPreflight(context.Background(), rollout.PreflightOptions{
		Workspace:  ws,
		DiskStatFn: lowFree,
	}); safepath.ExtractCode(err) != safepath.ErrPreflightFailed {
		t.Fatalf("expected GRABTAC-E101 when RunPreflight hits NVMe reserve, got: %v", err)
	}

	// Verify grabman doctor compatibility on a scaffolded workspace paired with vps-8c-16g compose
	grabmanBin := rollout.LocateGrabmanBinary("")
	if grabmanBin == "" {
		t.Fatalf("grabman CLI binary required for doctor parity test")
	}
	gmWS := filepath.Join(t.TempDir(), "bluehost-store")
	if out, err := exec.Command(grabmanBin, "init", "--dir", gmWS, "--profile", "ai-stack", "--name", "Bluehost Bookstore", "--edge-proxy", "nginx", "--json").CombinedOutput(); err != nil {
		t.Fatalf("grabman init failed: %v\n%s", err, string(out))
	}
	compose8c, _ := compose.RenderProfileCompose("vps-8c-16g", "bookstore.example.com")
	if err := os.WriteFile(filepath.Join(gmWS, "deploy", "docker-compose.yml"), compose8c, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(grabmanBin, "solve", "--dir", gmWS, "--json").CombinedOutput(); err != nil {
		t.Fatalf("grabman solve failed on vps-8c-16g compose: %v\n%s", err, string(out))
	}
	if out, err := exec.Command(grabmanBin, "doctor", "--dir", gmWS, "--json").CombinedOutput(); err != nil {
		t.Fatalf("grabman doctor failed on vps-8c-16g compose: %v\n%s", err, string(out))
	}
}

func TestContaboToBluehostMigrationExportImportAndStarterKit(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	srcWS := filepath.Join(tmp, "contabo-src")
	dstWS := filepath.Join(tmp, "bluehost-dst")

	writeSrc := func(rel, body string) {
		t.Helper()
		full := filepath.Join(srcWS, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSrc("grabman.lock", `{"lock_version":"1","digest":"ok"}`)
	writeSrc("storage/site-dna.json", `{"schema":"groxy-site-dna-v0.4.0","price":"14.99"}`)
	writeSrc("public/grex/tokens.css", ":root{--grex-primary:#047857;}\n")
	writeSrc("uploads/books/sample.epub", "EPUB-PK-BINARY-CONTENT")
	writeSrc(".env", "GRABLAB_MASTER_KEY=do_not_export_me\n")
	pgDump := filepath.Join(tmp, "pg18.sql")
	if err := os.WriteFile(pgDump, []byte("-- PostgreSQL 18 dump\nSELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundleTar := filepath.Join(tmp, "contabo-to-bluehost.tar.gz")
	man, err := snapshot.MigrateExport(ctx, snapshot.MigrateExportOptions{
		SourceWorkspace: srcWS,
		OutputBundle:    bundleTar,
		SourceProfile:   "vps-6c-12g",
		TargetProfile:   "vps-8c-16g",
		PostgresDump:    pgDump,
		BundleID:        "mig-test-1",
		Now:             func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("MigrateExport failed: %v", err)
	}
	if !man.HasPostgres || man.SourceProfile != "vps-6c-12g" || man.TargetProfile != "vps-8c-16g" {
		t.Fatalf("unexpected migration manifest: %+v", man)
	}
	for _, a := range man.Artifacts {
		if strings.Contains(a.RelPath, ".env") {
			t.Fatalf("migration bundle leaked sensitive .env file: %s", a.RelPath)
		}
	}

	// Import onto Bluehost vps-8c-16g target workspace
	imp, err := snapshot.MigrateImport(ctx, snapshot.MigrateImportOptions{
		BundlePath:      bundleTar,
		TargetWorkspace: dstWS,
		TargetProfile:   "vps-8c-16g",
	})
	if err != nil {
		t.Fatalf("MigrateImport failed: %v", err)
	}
	if !imp.Verified || !imp.Applied || !imp.ReadyForCutover || imp.PostgresDumpPath == "" {
		t.Fatalf("unexpected MigrateImportResult: %+v", imp)
	}
	restoredEPUB, err := os.ReadFile(filepath.Join(dstWS, "uploads", "books", "sample.epub"))
	if err != nil || string(restoredEPUB) != "EPUB-PK-BINARY-CONTENT" {
		t.Fatalf("restored upload mismatch: %v %q", err, string(restoredEPUB))
	}

	// Tampered directory bundle must fail closed with GRABTAC-E701
	bundleDir := filepath.Join(tmp, "bundle-dir")
	if _, err := snapshot.MigrateExport(ctx, snapshot.MigrateExportOptions{
		SourceWorkspace: srcWS,
		OutputBundle:    bundleDir,
		SourceProfile:   "vps-6c-12g",
		TargetProfile:   "vps-8c-16g",
	}); err != nil {
		t.Fatalf("MigrateExport dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "payload", "public", "grex", "tokens.css"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.MigrateImport(ctx, snapshot.MigrateImportOptions{
		BundlePath:      bundleDir,
		TargetWorkspace: filepath.Join(tmp, "dst-tamper"),
	}); safepath.ExtractCode(err) != safepath.ErrSnapshotFailed {
		t.Fatalf("expected GRABTAC-E701 on tampered migration bundle, got: %v", err)
	}

	// Verify vps-starter-kit.md and docs/vps-starter-kit.md exist, match byte-for-byte, and contain zero plaintext secrets
	rootKit, err := os.ReadFile(filepath.Join("..", "vps-starter-kit.md"))
	if err != nil {
		t.Fatalf("missing vps-starter-kit.md: %v", err)
	}
	docsKit, err := os.ReadFile(filepath.Join("..", "docs", "vps-starter-kit.md"))
	if err != nil {
		t.Fatalf("missing docs/vps-starter-kit.md: %v", err)
	}
	if string(rootKit) != string(docsKit) {
		t.Fatalf("vps-starter-kit.md and docs/vps-starter-kit.md drifted out of parity")
	}
	kitTxt := string(rootKit)
	for _, mustHave := range []string{
		"1.30.6",
		"573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62",
		"/_protected_media/",
		"/_protected_b2/",
		"aio threads;",
		"vps-6c-12g",
		"vps-8c-16g",
		"vps-8c-24g",
		"vps-8c-32g",
		"vps-16c-64g",
		"15 GB",
		"GRAB_B2_ENDPOINT",
		"reload-workers.sh",
		"grisp-backup.sh",
	} {
		if !strings.Contains(kitTxt, mustHave) {
			t.Fatalf("vps-starter-kit.md missing required section/token %q", mustHave)
		}
	}

	// Verify new CLI subcommands (edge, profile, migrate-export, migrate-import)
	bin := buildGrabtaculousCLI(t)
	cliOutDir := filepath.Join(tmp, "cli-render")
	out, code := runGrabtacCLI(t, bin, "edge", "render", "--profile", "vps-8c-16g", "--domain", "books.example.com", "--out-dir", cliOutDir, "--json")
	var edgeResp map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &edgeResp) != nil || edgeResp["ok"] != true {
		t.Fatalf("edge render CLI failed (code=%d): %s", code, out)
	}
	namedGrisp, _ := os.ReadFile(filepath.Join(cliOutDir, "etc", "nginx", "sites-available", "grisp.conf"))
	if !strings.Contains(string(namedGrisp), "server_name books.example.com www.books.example.com;") {
		t.Fatalf("expected domain + www server_name in named grisp.conf:\n%s", string(namedGrisp))
	}
	out, code = runGrabtacCLI(t, bin, "profile", "render", "vps-8c-16g", "--domain", "books.example.com", "--out-dir", cliOutDir, "--json")
	if code != 0 || !strings.Contains(out, "docker-compose.yml") {
		t.Fatalf("profile render CLI failed (code=%d): %s", code, out)
	}
}

func TestDomainFreeDefaultGrispConfAndGrexWelcomePage(t *testing.T) {
	opts := edge.DefaultNginxConfigOptions("")
	if opts.Domain != "_" {
		t.Fatalf("expected default domain-free mode '_', got %q", opts.Domain)
	}
	grispBytes, err := edge.RenderGrispSiteConf(opts)
	if err != nil {
		t.Fatalf("RenderGrispSiteConf default mode failed: %v", err)
	}
	grisp := string(grispBytes)
	for _, bad := range []string{
		"allenmiles.com",
		"urbanspress.com",
		"bookstore.example.com",
		"/etc/letsencrypt/live/_/",
		"grisp-default.crt",
		"grisp-default.key",
	} {
		if strings.Contains(grisp, bad) {
			t.Fatalf("domain-free grisp.conf leaked hardcoded domain/cert %q:\n%s", bad, grisp)
		}
	}
	for _, want := range []string{
		"listen 80 default_server;",
		"listen [::]:80 default_server;",
		"return 444;",
		"listen 443 ssl default_server;",
		"listen [::]:443 ssl default_server;",
		"ssl_reject_handshake on;",
		"server_name _;",
		"server_name localhost 127.0.0.1;",
		"allow 127.0.0.1;",
		"allow ::1;",
		"deny all;",
		"root /var/www/grisp-welcome;",
		"location ^~ /grex/",
		"location ~ ^/_(health|ready)$",
	} {
		if !strings.Contains(grisp, want) {
			t.Fatalf("domain-free grisp.conf missing %q:\n%s", want, grisp)
		}
	}

	// Verify explicit domain HTTP-only mode (--domain urbanspress.com --http-only)
	httpOnlyOpts := edge.DefaultNginxConfigOptions("urbanspress.com")
	httpOnlyOpts.HTTPOnly = true
	httpOnlyBytes, err := edge.RenderGrispSiteConf(httpOnlyOpts)
	if err != nil {
		t.Fatalf("RenderGrispSiteConf HTTPOnly failed: %v", err)
	}
	httpOnlyConf := string(httpOnlyBytes)
	for _, want := range []string{
		"listen 80;",
		"listen [::]:80;",
		"server_name urbanspress.com www.urbanspress.com;",
		"location ^~ /.well-known/acme-challenge/",
		"proxy_pass http://grisp_backend;",
		"location ^~ /_protected_media/",
	} {
		if !strings.Contains(httpOnlyConf, want) {
			t.Fatalf("HTTP-only domain grisp.conf missing %q:\n%s", want, httpOnlyConf)
		}
	}
	for _, bad := range []string{"default_server", "ssl_certificate", "return 301 https://", "/var/www/grisp-welcome"} {
		if strings.Contains(httpOnlyConf, bad) {
			t.Fatalf("HTTP-only domain grisp.conf must not contain %q:\n%s", bad, httpOnlyConf)
		}
	}

	// Verify explicit domain HTTPS + HTTP/3 mode (--domain allenmiles.com)
	tlsOpts := edge.DefaultNginxConfigOptions("allenmiles.com")
	tlsBytes, err := edge.RenderGrispSiteConf(tlsOpts)
	if err != nil {
		t.Fatalf("RenderGrispSiteConf TLS mode failed: %v", err)
	}
	tlsConf := string(tlsBytes)
	for _, want := range []string{
		"server_name allenmiles.com www.allenmiles.com;",
		"listen 443 quic reuseport;",
		"http2 on;",
		"ssl_certificate /etc/letsencrypt/live/allenmiles.com/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/allenmiles.com/privkey.pem;",
		"proxy_pass http://grisp_backend;",
	} {
		if !strings.Contains(tlsConf, want) {
			t.Fatalf("TLS domain grisp.conf missing %q:\n%s", want, tlsConf)
		}
	}
	if strings.Contains(tlsConf, "default_server") || strings.Contains(tlsConf, "/var/www/grisp-welcome") {
		t.Fatalf("TLS domain grisp.conf must not contain default_server or welcome root:\n%s", tlsConf)
	}

	welcomeBytes, err := edge.RenderGrispWelcomeHTML(opts)
	if err != nil {
		t.Fatalf("RenderGrispWelcomeHTML failed: %v", err)
	}
	welcomeHTML := string(welcomeBytes)
	if strings.Contains(strings.ToLower(welcomeHTML), "<div") || strings.Contains(strings.ToLower(welcomeHTML), "gsap") {
		t.Fatalf("Grisp welcome HTML must contain zero <div> tags and zero GSAP bloat:\n%s", welcomeHTML)
	}
	for _, want := range []string{
		"<canvas id=\"grex-welcome-canvas\"",
		"/grex/tokens.css",
		"/grex/welcome.css",
		"/grex/welcome-grid.js",
		"Welcome to Grisp.",
		"6 Loopback Workers Configured",
		"127.0.0.1:8765-8770",
		"grabman init",
		"grab serve",
	} {
		if !strings.Contains(welcomeHTML, want) {
			t.Fatalf("Grisp welcome HTML missing %q", want)
		}
	}

	// Verify `grabtaculous edge render` (without --domain) produces catch-all 444 + loopback grisp.conf & Grex welcome assets
	bin := buildGrabtaculousCLI(t)
	defaultOut := filepath.Join(t.TempDir(), "default-edge")
	out, code := runGrabtacCLI(t, bin, "edge", "render", "--profile", "vps-8c-16g", "--out-dir", defaultOut, "--json")
	if code != 0 {
		t.Fatalf("default edge render CLI failed (code=%d): %s", code, out)
	}
	for _, rel := range []string{
		"etc/nginx/sites-available/grisp.conf",
		"var/www/grisp-welcome/index.html",
		"var/www/grisp-welcome/grex/welcome.css",
		"var/www/grisp-welcome/grex/welcome-grid.js",
		"var/www/grisp-welcome/grex/grex.js",
		"var/www/grisp-welcome/grex/tokens.css",
	} {
		if st, err := os.Stat(filepath.Join(defaultOut, filepath.FromSlash(rel))); err != nil || st.Size() == 0 {
			t.Fatalf("expected non-empty rendered edge file %s (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(defaultOut, "etc", "nginx", "sites-available", "wordpress-fpm.conf")); err == nil {
		t.Fatalf("did not expect wordpress-fpm.conf when no domain or --wp-domain was supplied")
	}

	// Verify CLI `--domain urbanspress.com --http-only --ports 8771,8772 --upstream-name urbanspress_backend`
	httpOutDir := filepath.Join(t.TempDir(), "http-only-edge")
	out, code = runGrabtacCLI(t, bin, "edge", "render", "--profile", "vps-8c-16g", "--domain", "urbanspress.com", "--http-only", "--ports", "8771,8772", "--upstream-name", "urbanspress_backend", "--out-dir", httpOutDir, "--json")
	if code != 0 {
		t.Fatalf("edge render --http-only CLI failed (code=%d): %s", code, out)
	}
	renderedSite, err := os.ReadFile(filepath.Join(httpOutDir, "etc", "nginx", "sites-available", "grisp.conf"))
	if err != nil {
		t.Fatal(err)
	}
	renderedSiteStr := string(renderedSite)
	for _, want := range []string{
		"upstream urbanspress_backend {",
		"server 127.0.0.1:8771 ",
		"server 127.0.0.1:8772 ",
		"server_name urbanspress.com www.urbanspress.com;",
		"proxy_pass http://urbanspress_backend;",
	} {
		if !strings.Contains(renderedSiteStr, want) {
			t.Fatalf("HTTP-only CLI grisp.conf missing %q:\n%s", want, renderedSiteStr)
		}
	}
}
