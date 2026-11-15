package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grabtaculous/internal/container"
	"grabtaculous/internal/safepath"
)

func validHardenedCompose() string {
	return `services:
  grab-app:
    image: ghcr.io/urbanspress/grab:v1.1.0@sha256:4d5f9a83a91d7b1e39ce19ab818373a7f244ca4b2a9d2218c98eb74bc7211100
    read_only: true
    user: "65532:65532"
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=64m
    ports:
      - "127.0.0.1:8765:8080"
    volumes:
      - ../src:/workspace/src:ro
      - app-storage:/workspace/storage:rw
    environment:
      GRAB_APP_SECRET: "${GRAB_APP_SECRET:?Set GRAB_APP_SECRET}"
`
}

func TestContainerHardeningRejectsInsecurePolicies(t *testing.T) {
	base := validHardenedCompose()
	if err := container.AuditComposeYAML("deploy/docker-compose.yml", base, container.AuditOptions{RequireDigest: true}); err != nil {
		t.Fatalf("expected valid hardened Compose to pass, got: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(string) string
		wantMsg string
	}{
		{
			name:    "read_only_false",
			mutate:  func(s string) string { return strings.Replace(s, "read_only: true", "read_only: false", 1) },
			wantMsg: "read_only: true",
		},
		{
			name:    "missing_cap_drop_all",
			mutate:  func(s string) string { return strings.Replace(s, "- ALL", "- NET_BIND_SERVICE", 1) },
			wantMsg: "cap_drop",
		},
		{
			name:    "missing_no_new_privileges",
			mutate:  func(s string) string { return strings.Replace(s, "no-new-privileges:true", "seccomp:unconfined", 1) },
			wantMsg: "no-new-privileges",
		},
		{
			name:    "root_uid_0",
			mutate:  func(s string) string { return strings.Replace(s, `"65532:65532"`, `"0:0"`, 1) },
			wantMsg: "UID/GID 0",
		},
		{
			name:    "root_username",
			mutate:  func(s string) string { return strings.Replace(s, `"65532:65532"`, `"root"`, 1) },
			wantMsg: "root",
		},
		{
			name:    "latest_tag_forbidden",
			mutate:  func(s string) string { return strings.Replace(s, "v1.1.0@sha256:4d5f9a83a91d7b1e39ce19ab818373a7f244ca4b2a9d2218c98eb74bc7211100", "latest", 1) },
			wantMsg: ":latest",
		},
		{
			name:    "missing_digest_pin",
			mutate:  func(s string) string { return strings.Replace(s, "@sha256:4d5f9a83a91d7b1e39ce19ab818373a7f244ca4b2a9d2218c98eb74bc7211100", "", 1) },
			wantMsg: "@sha256",
		},
		{
			name:    "privileged_true",
			mutate:  func(s string) string { return strings.Replace(s, "read_only: true", "read_only: true\n    privileged: true", 1) },
			wantMsg: "privileged: true",
		},
		{
			name:    "host_network",
			mutate:  func(s string) string { return strings.Replace(s, "read_only: true", "read_only: true\n    network_mode: host", 1) },
			wantMsg: "network_mode: host",
		},
		{
			name:    "docker_socket_mount",
			mutate:  func(s string) string { return strings.Replace(s, "../src:/workspace/src:ro", "/var/run/docker.sock:/var/run/docker.sock", 1) },
			wantMsg: "Docker socket",
		},
		{
			name:    "public_0000_db_port",
			mutate:  func(s string) string { return strings.Replace(s, "127.0.0.1:8765:8080", "0.0.0.0:5432:5432", 1) },
			wantMsg: "non-loopback port",
		},
		{
			name:    "bare_host_port_exposure",
			mutate:  func(s string) string { return strings.Replace(s, "127.0.0.1:8765:8080", "8080:8080", 1) },
			wantMsg: "non-loopback port",
		},
		{
			name:    "tmpfs_missing_noexec",
			mutate:  func(s string) string { return strings.Replace(s, "/tmp:rw,noexec,nosuid,size=64m", "/tmp:rw,size=64m", 1) },
			wantMsg: "noexec and nosuid",
		},
		{
			name: "plaintext_secret_redacted",
			mutate: func(s string) string {
				return strings.Replace(s, `"${GRAB_APP_SECRET:?Set GRAB_APP_SECRET}"`, `"super_plaintext_secret_98765"`, 1)
			},
			wantMsg: "redacted",
		},
		{
			name: "embedded_dsn_password",
			mutate: func(s string) string {
				return s + "      DATABASE_URL: \"postgres://dbuser:secretpass@db:5432/app\"\n"
			},
			wantMsg: "redacted",
		},
		{
			name: "private_ipv4_literal",
			mutate: func(s string) string {
				return s + "      UPSTREAM_HOST: \"192.168.10.42\"\n"
			},
			wantMsg: "private IPv4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := container.AuditComposeYAML("deploy/docker-compose.yml", tc.mutate(base), container.AuditOptions{RequireDigest: true})
			if err == nil {
				t.Fatalf("expected GRABTAC-E401 error for %s, got nil", tc.name)
			}
			if safepath.ExtractCode(err) != safepath.ErrSecurityPolicy {
				t.Fatalf("expected code %s, got %s (%v)", safepath.ErrSecurityPolicy, safepath.ExtractCode(err), err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("expected error to mention %q, got: %v", tc.wantMsg, err)
			}
			if strings.Contains(err.Error(), "super_plaintext_secret_98765") || strings.Contains(err.Error(), "secretpass") || strings.Contains(err.Error(), "192.168.10.42") {
				t.Fatalf("error leaked secret/private IP literal: %v", err)
			}
		})
	}
}

func TestEdgeProxyConfigRendering(t *testing.T) {
	// Reject non-loopback upstream
	if _, err := container.RenderEdgeConfig(container.EdgeRouteConfig{
		EdgeProxy:     "caddy",
		Domain:        "shop.example.com",
		UpstreamHost:  "0.0.0.0",
		UpstreamPorts: []int{8765},
	}); safepath.ExtractCode(err) != safepath.ErrSecurityPolicy {
		t.Fatalf("expected GRABTAC-E401 for non-loopback upstream, got: %v", err)
	}

	// Caddy (default)
	caddyArt, err := container.RenderEdgeConfig(container.EdgeRouteConfig{
		EdgeProxy:     "caddy",
		Domain:        "shop.example.com",
		UpstreamHost:  "127.0.0.1",
		UpstreamPorts: []int{8765, 8766},
		HealthPath:    "/_ready",
	})
	if err != nil {
		t.Fatalf("caddy render failed: %v", err)
	}
	caddyText := string(caddyArt.Content)
	if caddyArt.Filename != "deploy/Caddyfile" || !strings.Contains(caddyText, "reverse_proxy 127.0.0.1:8765 127.0.0.1:8766") || !strings.Contains(caddyText, "health_uri /_ready") {
		t.Fatalf("unexpected Caddyfile output:\n%s", caddyText)
	}

	// Nginx + Certbot
	nginxArt, err := container.RenderEdgeConfig(container.EdgeRouteConfig{
		EdgeProxy:     "nginx",
		Domain:        "shop.example.com",
		UpstreamHost:  "127.0.0.1",
		UpstreamPorts: []int{8765},
	})
	if err != nil {
		t.Fatalf("nginx render failed: %v", err)
	}
	nginxText := string(nginxArt.Content)
	if nginxArt.Filename != "deploy/nginx.conf" || !strings.Contains(nginxText, "server 127.0.0.1:8765") || !strings.Contains(nginxText, "/.well-known/acme-challenge/") {
		t.Fatalf("unexpected nginx.conf output:\n%s", nginxText)
	}

	// Cloudflare Tunnel
	cfArt, err := container.RenderEdgeConfig(container.EdgeRouteConfig{
		EdgeProxy:     "cloudflare-tunnel",
		Domain:        "shop.example.com",
		UpstreamHost:  "127.0.0.1",
		UpstreamPorts: []int{8765},
	})
	if err != nil {
		t.Fatalf("cloudflared render failed: %v", err)
	}
	cfText := string(cfArt.Content)
	if cfArt.Filename != "deploy/cloudflared.yml" || !strings.Contains(cfText, "http://127.0.0.1:8765") {
		t.Fatalf("unexpected cloudflared.yml output:\n%s", cfText)
	}
}

func TestSafePathConfinementParity(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{"../escape.txt", ".env", ".env.production", ".git/config", ".ssh/id_rsa", "keys/site.pem", "certs/priv.key"} {
		if _, _, err := safepath.ResolveFile(ws, blocked, safepath.ResolveOptions{AllowMissing: true}); safepath.ExtractCode(err) != safepath.ErrPathConfinement {
			t.Fatalf("expected GRABTAC-E201 for %q, got %v", blocked, err)
		}
	}
	if _, _, err := safepath.ResolveFile(ws, "deploy/.env.example", safepath.ResolveOptions{AllowMissing: true, AllowEnvExample: true}); err != nil {
		t.Fatalf("expected deploy/.env.example to be allowed when AllowEnvExample=true, got: %v", err)
	}
}
