package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grabtaculous/internal/catalog"
	"grabtaculous/internal/container"
	"grabtaculous/internal/daemon"
	"grabtaculous/internal/safepath"
)

func TestCuratedMarketplaceBlueprintsAndHardenedComposeRendering(t *testing.T) {
	loader := catalog.NewLoader("")
	entries, err := loader.List()
	if err != nil {
		t.Fatalf("catalog.List failed: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("expected 10 curated blueprints, got %d", len(entries))
	}

	requiredIDs := map[string]bool{
		"grablab-grex-bundle": false,
		"grablab-full-stack":  false,
		"grab-grisp-app":      false,
		"postgres-18":         false,
		"mariadb-11-8":        false,
		"redis-standalone":    false,
		"analytics":           false,
		"object-storage":      false,
		"automation":          false,
		"webmail":             false,
	}

	for _, entry := range entries {
		bp, err := loader.LoadBlueprint(entry.ID)
		if err != nil {
			t.Fatalf("LoadBlueprint(%q) failed: %v", entry.ID, err)
		}
		requiredIDs[entry.ID] = true

		composeBytes, err := container.RenderComposeYAML(bp.Stack)
		if err != nil {
			t.Fatalf("RenderComposeYAML(%q) failed: %v", entry.ID, err)
		}
		if err := container.AuditComposeYAML(entry.ID+".yml", string(composeBytes), container.AuditOptions{RequireDigest: true}); err != nil {
			t.Fatalf("AuditComposeYAML(%q) failed: %v", entry.ID, err)
		}
		envBytes := container.RenderEnvExample(bp.Stack)
		if issues := container.ScanNoPlaintextSecrets(entry.ID+".env.example", string(envBytes)); len(issues) > 0 {
			t.Fatalf("RenderEnvExample(%q) contained secret/IP issues: %v", entry.ID, issues)
		}
	}

	for id, seen := range requiredIDs {
		if !seen {
			t.Fatalf("missing required curated marketplace blueprint %q", id)
		}
	}

	// Verify unknown blueprint fails closed with GRABTAC-E301
	if _, err := loader.LoadBlueprint("untrusted-curl-script"); safepath.ExtractCode(err) != safepath.ErrCatalogInvalid {
		t.Fatalf("expected GRABTAC-E301 for unknown blueprint, got: %v", err)
	}
}

func TestCatalogTamperDetection(t *testing.T) {
	tmp := t.TempDir()
	// Copy catalog into temp dir and mutate one byte of grab-grisp-app.json
	diskCatalog := filepath.Join(tmp, "catalog")
	if err := os.MkdirAll(filepath.Join(diskCatalog, "companions"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcRoot := filepath.Join("..", "catalog")
	rawIdx, err := os.ReadFile(filepath.Join(srcRoot, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diskCatalog, "index.json"), rawIdx, 0o644); err != nil {
		t.Fatal(err)
	}
	rawApp, err := os.ReadFile(filepath.Join(srcRoot, "grab-grisp-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(rawApp), `"65532:65532"`, `"0:0"`, 1)
	if err := os.WriteFile(filepath.Join(diskCatalog, "grab-grisp-app.json"), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := catalog.NewLoader(diskCatalog)
	_, err = loader.LoadBlueprint("grab-grisp-app")
	if err == nil || safepath.ExtractCode(err) != safepath.ErrCatalogInvalid {
		t.Fatalf("expected GRABTAC-E301 tamper error, got: %v", err)
	}
}

func TestDeployCuratedBlueprintsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	for _, tc := range []struct {
		blueprint string
		edge      string
		edgeFile  string
	}{
		{"grablab-grex-bundle", "caddy", "deploy/Caddyfile"},
		{"grablab-full-stack", "nginx", "deploy/nginx.conf"},
		{"grab-grisp-app", "cloudflare-tunnel", "deploy/cloudflared.yml"},
	} {
		ws := filepath.Join(tmp, tc.blueprint)
		res, err := daemon.DeployBlueprint(ctx, daemon.DeployOptions{
			BlueprintID: tc.blueprint,
			TargetDir:   ws,
			Domain:      "site.example.com",
			EdgeProxy:   tc.edge,
		})
		if err != nil {
			t.Fatalf("DeployBlueprint(%s) failed: %v", tc.blueprint, err)
		}
		if res.EdgeFile != tc.edgeFile {
			t.Fatalf("expected edge file %q, got %q", tc.edgeFile, res.EdgeFile)
		}
		if len(res.WorkerPorts) == 0 || res.WorkerPorts[0] < 8765 || res.WorkerPorts[0] > 8772 {
			t.Fatalf("expected loopback worker ports in 8765-8772, got %v", res.WorkerPorts)
		}
		composeBytes, err := os.ReadFile(filepath.Join(ws, "deploy", "docker-compose.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := container.AuditComposeYAML("deploy/docker-compose.yml", string(composeBytes), container.AuditOptions{RequireDigest: true}); err != nil {
			t.Fatalf("deployed compose failed audit for %s: %v", tc.blueprint, err)
		}
	}
}

func TestUpstreamEcosystemComposeContracts(t *testing.T) {
	// Verify frozen upstream v1.x compose templates match Grabtaculous --read-only & loopback contracts.
	upstreams := []string{
		filepath.Join(`C:\Development\grablab-cms`, "deploy", "docker-compose.yml"),
		filepath.Join(`C:\Development\grex-builder`, "deploy", "docker-compose.yml"),
		filepath.Join(`C:\Development\groxy-ai`, "deploy", "docker-compose.yml"),
	}
	for _, p := range upstreams {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("expected upstream compose file %s: %v", p, err)
		}
		services, err := container.ParseComposeServices(string(raw))
		if err != nil {
			t.Fatalf("failed parsing upstream compose %s: %v", p, err)
		}
		if len(services) == 0 {
			t.Fatalf("upstream %s had 0 services", p)
		}
		for _, svc := range services {
			if !svc.ReadOnly {
				t.Fatalf("%s service %s missing read_only: true", p, svc.Name)
			}
			for _, port := range svc.Ports {
				if !strings.HasPrefix(port, "127.0.0.1:") {
					t.Fatalf("%s service %s binds non-loopback port %q", p, svc.Name, port)
				}
			}
		}
		if issues := container.ScanNoPlaintextSecrets(p, string(raw)); len(issues) > 0 {
			t.Fatalf("%s failed secret/IP scan: %v", p, issues)
		}
	}

	// Ensure catalog index JSON encodes cleanly
	rawIdx, err := os.ReadFile(filepath.Join("..", "catalog", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx catalog.CatalogIndex
	if err := json.Unmarshal(rawIdx, &idx); err != nil {
		t.Fatal(err)
	}
}
