package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"grabtaculous/internal/daemon"
	"grabtaculous/internal/safepath"
)

func TestDaemonLoopbackBindingAndControlAPI(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")

	// Reject any public 0.0.0.0 / wildcard / non-loopback bind attempt (ADR-005 / GRABTAC-E501)
	for _, badAddr := range []string{"0.0.0.0:9440", ":9440", "[::]:9440", "192.168.1.50:9440", "10.0.0.5:9440"} {
		if _, err := daemon.NewServer(daemon.ServerConfig{ListenAddr: badAddr, StateDir: stateDir}); safepath.ExtractCode(err) != safepath.ErrControlPlane {
			t.Fatalf("expected GRABTAC-E501 rejecting %q, got: %v", badAddr, err)
		}
	}

	srv, err := daemon.NewServer(daemon.ServerConfig{
		ListenAddr:   "127.0.0.1:0",
		StateDir:     stateDir,
		ControlToken: "test-control-token-secret",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	var baseURL string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.BoundAddress(); addr != "" {
			baseURL = "http://" + addr
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if baseURL == "" {
		t.Fatalf("daemon did not bind loopback port in time")
	}

	client := &http.Client{Timeout: 3 * time.Second}
	// /_health & /_ready succeed
	for _, ep := range []string{"/_health", "/_ready"} {
		resp, err := client.Get(baseURL + ep)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on %s, err=%v resp=%+v", ep, err, resp)
		}
		_ = resp.Body.Close()
	}

	// Unauthenticated /v1/* rejected with 401
	unauthResp, err := client.Get(baseURL + "/v1/nodes")
	if err != nil || unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /v1/nodes, err=%v resp=%+v", err, unauthResp)
	}
	_ = unauthResp.Body.Close()

	// Authenticated /v1/catalog & /v1/deploy & /v1/snapshots & /v1/rollback
	authDo := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, baseURL+path, &buf)
		req.Header.Set("Authorization", "Bearer "+srv.ControlToken())
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, path, err)
		}
		defer resp.Body.Close()
		var parsed map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&parsed)
		return resp.StatusCode, parsed
	}

	code, catResp := authDo(http.MethodGet, "/v1/catalog", nil)
	if code != 200 || catResp["ok"] != true {
		t.Fatalf("GET /v1/catalog failed (%d): %+v", code, catResp)
	}

	appWS := filepath.Join(t.TempDir(), "daemon-app")
	code, depResp := authDo(http.MethodPost, "/v1/deploy", map[string]any{
		"blueprint_id": "grab-grisp-app",
		"workspace":    appWS,
		"app_name":     "daemon-grisp",
		"domain":       "app.example.com",
		"edge_proxy":   "caddy",
		"workers":      2,
	})
	if code != 200 || depResp["ok"] != true {
		t.Fatalf("POST /v1/deploy failed (%d): %+v", code, depResp)
	}
	depMap := depResp["deploy"].(map[string]any)
	deployID := depMap["deploy_snapshot_id"].(string)

	// Verify port collision prevention across multiple apps within 8765-8772
	store, err := daemon.NewInventoryStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.AllocatePorts("second-app", 2)
	if err != nil {
		t.Fatal(err)
	}
	if p2[0] != 8767 || p2[1] != 8768 {
		t.Fatalf("expected non-overlapping worker ports [8767 8768], got %v", p2)
	}

	// Rollback via API
	code, rbResp := authDo(http.MethodPost, "/v1/rollback", map[string]any{
		"deploy_id": deployID,
		"workspace": appWS,
	})
	if code != 200 || rbResp["ok"] != true {
		t.Fatalf("POST /v1/rollback failed (%d): %+v", code, rbResp)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("daemon shutdown error: %v", err)
	}
}

func buildGrabtaculousCLI(t *testing.T) string {
	t.Helper()
	binName := "grabtaculous"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	cmd := exec.Command("go", "build", "-o", binPath, "../cmd/grabtaculous")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build grabtaculous CLI: %v\n%s", err, string(out))
	}
	return binPath
}

func runGrabtacCLI(t *testing.T, binPath string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("failed running CLI %v: %v\n%s", args, err, string(out))
	return string(out), 1
}

func TestGrabtaculousCLIEndToEnd(t *testing.T) {
	bin := buildGrabtaculousCLI(t)
	tmp := t.TempDir()

	// 1. version --json
	out, code := runGrabtacCLI(t, bin, "version", "--json")
	if code != 0 || !strings.Contains(out, `"version": "0.1.0"`) {
		t.Fatalf("version --json failed (code=%d): %s", code, out)
	}

	// 2. daemon rejects 0.0.0.0 binding fail-closed
	out, code = runGrabtacCLI(t, bin, "daemon", "--listen", "0.0.0.0:9440", "--state-dir", filepath.Join(tmp, "dstate"), "--json")
	if code == 0 || !strings.Contains(out, "GRABTAC-E501") {
		t.Fatalf("expected non-zero GRABTAC-E501 for 0.0.0.0 bind (code=%d): %s", code, out)
	}

	// 3. catalog list & inspect
	out, code = runGrabtacCLI(t, bin, "catalog", "list", "--json")
	if code != 0 || !strings.Contains(out, "grablab-grex-bundle") || !strings.Contains(out, "postgres-18") {
		t.Fatalf("catalog list failed (code=%d): %s", code, out)
	}
	out, code = runGrabtacCLI(t, bin, "catalog", "inspect", "grablab-full-stack", "--json")
	if code != 0 || !strings.Contains(out, "groxy-ai") {
		t.Fatalf("catalog inspect failed (code=%d): %s", code, out)
	}

	// 4. deploy + audit + snapshot verify + rollback
	ws := filepath.Join(tmp, "cli-site")
	out, code = runGrabtacCLI(t, bin, "deploy", "grablab-grex-bundle", "--dir", ws, "--domain", "blog.example.com", "--edge-proxy", "caddy", "--json")
	if code != 0 || !strings.Contains(out, `"deploy_snapshot_id"`) {
		t.Fatalf("deploy failed (code=%d): %s", code, out)
	}

	out, code = runGrabtacCLI(t, bin, "audit", "--dir", ws, "--require-digest", "--json")
	if code != 0 || !strings.Contains(out, `"ok": true`) {
		t.Fatalf("audit failed (code=%d): %s", code, out)
	}

	var parsed map[string]any
	// Parse snapshot list to extract deploy_id
	out, code = runGrabtacCLI(t, bin, "snapshot", "list", "--dir", ws, "--json")
	if code != 0 || json.Unmarshal([]byte(out), &parsed) != nil {
		t.Fatalf("snapshot list failed (code=%d): %s", code, out)
	}
	snaps := parsed["snapshots"].([]any)
	snapID := snaps[0].(map[string]any)["deploy_id"].(string)

	out, code = runGrabtacCLI(t, bin, "snapshot", "verify", snapID, "--dir", ws, "--json")
	if code != 0 || !strings.Contains(out, `"verified": true`) {
		t.Fatalf("snapshot verify failed (code=%d): %s", code, out)
	}

	// Mutate Caddyfile and verify rollback restores it
	caddyPath := filepath.Join(ws, "deploy", "Caddyfile")
	origCaddy, _ := os.ReadFile(caddyPath)
	_ = os.WriteFile(caddyPath, []byte("broken caddy"), 0o644)

	out, code = runGrabtacCLI(t, bin, "rollback", snapID, "--dir", ws, "--json")
	if code != 0 || !strings.Contains(out, `"restored": true`) {
		t.Fatalf("rollback failed (code=%d): %s", code, out)
	}
	restoredCaddy, _ := os.ReadFile(caddyPath)
	if string(restoredCaddy) != string(origCaddy) {
		t.Fatalf("expected Caddyfile restored byte-for-byte")
	}

	out, code = runGrabtacCLI(t, bin, "status", "--dir", ws, "--json")
	if code != 0 || !strings.Contains(out, `"app_count": 1`) {
		t.Fatalf("status failed (code=%d): %s", code, out)
	}
}
