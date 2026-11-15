package tests

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grabtaculous/internal/container"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
)

type mockGrispWorker struct {
	slot       rollout.WorkerSlot
	server     *httptest.Server
	ready      atomic.Bool
	generation string
	inflight   sync.WaitGroup
}

func startMockGrispWorker(t *testing.T, generation string, readyInitially bool) *mockGrispWorker {
	t.Helper()
	mw := &mockGrispWorker{generation: generation}
	mw.ready.Store(readyInitially)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /_ready", func(w http.ResponseWriter, _ *http.Request) {
		if !mw.ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ready":false}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	})
	mux.HandleFunc("GET /assets/site.css", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`body{color:#047857}`))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		mw.inflight.Add(1)
		defer mw.inflight.Done()
		if r.URL.Query().Get("slow") == "1" {
			time.Sleep(120 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "gen=%s", mw.generation)
	})

	srv := httptest.NewServer(mux)
	mw.server = srv
	u, _ := url.Parse(srv.URL)
	_, portStr, _ := net.SplitHostPort(u.Host)
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	mw.slot = rollout.WorkerSlot{
		Port:       port,
		BaseURL:    srv.URL,
		Generation: generation,
	}
	return mw
}

func TestZeroDowntimeGrispRollingSwapAndReadyGating(t *testing.T) {
	old1 := startMockGrispWorker(t, "v1", true)
	old2 := startMockGrispWorker(t, "v1", true)
	defer old1.server.Close()
	defer old2.server.Close()

	cand1 := startMockGrispWorker(t, "v2", false)
	cand2 := startMockGrispWorker(t, "v2", false)
	defer cand1.server.Close()
	defer cand2.server.Close()

	// Fronting edge reverse proxy initially pointing at old workers, tracking in-flight proxy dispatches per generation.
	type upstreamGen struct {
		targets  []*url.URL
		inflight sync.WaitGroup
	}
	var upMu sync.RWMutex
	oldTargets := []*url.URL{}
	for _, s := range []string{old1.slot.BaseURL, old2.slot.BaseURL} {
		u, _ := url.Parse(s)
		oldTargets = append(oldTargets, u)
	}
	currentGen := &upstreamGen{targets: oldTargets}
	var prevGenMu sync.Mutex
	var prevGen *upstreamGen

	var rrCounter atomic.Uint64
	edgeProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upMu.RLock()
		gen := currentGen
		gen.inflight.Add(1)
		upMu.RUnlock()
		defer gen.inflight.Done()

		idx := rrCounter.Add(1) % uint64(len(gen.targets))
		rp := httputil.NewSingleHostReverseProxy(gen.targets[idx])
		rp.ServeHTTP(w, r)
	}))
	defer edgeProxy.Close()

	// Blast continuous concurrent requests through edgeProxy while rolling swap occurs
	ctx, cancel := context.WithCancel(context.Background())
	var totalReqs atomic.Int64
	var failedReqs atomic.Int64
	var sawV2 atomic.Int64
	var loadWG sync.WaitGroup
	client := &http.Client{Timeout: 2 * time.Second}

	for g := 0; g < 6; g++ {
		loadWG.Add(1)
		go func(workerID int) {
			defer loadWG.Done()
			for ctx.Err() == nil {
				target := edgeProxy.URL + "/"
				if workerID == 0 {
					target += "?slow=1"
				}
				resp, err := client.Get(target)
				totalReqs.Add(1)
				if err != nil {
					failedReqs.Add(1)
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					failedReqs.Add(1)
				} else if strings.Contains(string(body), "gen=v2") {
					sawV2.Add(1)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}(g)
	}

	// Let initial traffic warm up, then promote candidates to /_ready === 200 after 80ms
	time.Sleep(40 * time.Millisecond)
	go func() {
		time.Sleep(80 * time.Millisecond)
		cand1.ready.Store(true)
		cand2.ready.Store(true)
	}()

	swapRes, err := rollout.RollingSwap(context.Background(), rollout.RollingSwapConfig{
		OldWorkers:       []rollout.WorkerSlot{old1.slot, old2.slot},
		NewWorkers:       []rollout.WorkerSlot{cand1.slot, cand2.slot},
		ReadyTimeout:     5 * time.Second,
		DrainTimeout:     5 * time.Second,
		PollInterval:     15 * time.Millisecond,
		AssetParityPaths: []string{"/assets/site.css?v=abc123"},
		SwitchUpstream: func(_ context.Context, active []rollout.WorkerSlot) error {
			var next []*url.URL
			for _, w := range active {
				u, _ := url.Parse(w.BaseURL)
				next = append(next, u)
			}
			nextGen := &upstreamGen{targets: next}
			upMu.Lock()
			retired := currentGen
			currentGen = nextGen
			upMu.Unlock()
			prevGenMu.Lock()
			prevGen = retired
			prevGenMu.Unlock()
			return nil
		},
		DrainAndStop: func(_ context.Context, slot rollout.WorkerSlot, _ time.Duration) error {
			prevGenMu.Lock()
			retired := prevGen
			prevGenMu.Unlock()
			if retired != nil {
				retired.inflight.Wait()
			}
			if slot.Port == old1.slot.Port {
				old1.inflight.Wait()
				old1.server.Close()
			} else if slot.Port == old2.slot.Port {
				old2.inflight.Wait()
				old2.server.Close()
			}
			return nil
		},
	})
	if err != nil {
		cancel()
		loadWG.Wait()
		t.Fatalf("RollingSwap failed: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	cancel()
	loadWG.Wait()

	if !swapRes.AssetParityCheck || len(swapRes.PromotedWorkers) != 2 || len(swapRes.DrainedWorkers) != 2 {
		t.Fatalf("unexpected RollingSwapResult: %+v", swapRes)
	}
	if failedReqs.Load() != 0 {
		t.Fatalf("zero-downtime rollout dropped %d/%d requests", failedReqs.Load(), totalReqs.Load())
	}
	if totalReqs.Load() < 20 || sawV2.Load() == 0 {
		t.Fatalf("expected active traffic across old and v2 generations (total=%d, v2=%d)", totalReqs.Load(), sawV2.Load())
	}
}

func TestFailedCandidateKeepsLastKnownGoodActive(t *testing.T) {
	oldW := startMockGrispWorker(t, "v1", true)
	defer oldW.server.Close()
	badCand := startMockGrispWorker(t, "v2-broken", false) // permanently 503 on /_ready
	defer badCand.server.Close()

	switched := false
	drained := false
	res, err := rollout.RollingSwap(context.Background(), rollout.RollingSwapConfig{
		OldWorkers:   []rollout.WorkerSlot{oldW.slot},
		NewWorkers:   []rollout.WorkerSlot{badCand.slot},
		ReadyTimeout: 250 * time.Millisecond,
		PollInterval: 20 * time.Millisecond,
		SwitchUpstream: func(_ context.Context, _ []rollout.WorkerSlot) error {
			switched = true
			return nil
		},
		DrainAndStop: func(_ context.Context, slot rollout.WorkerSlot, _ time.Duration) error {
			if slot.Port == oldW.slot.Port {
				drained = true
			}
			return nil
		},
	})
	if err == nil || safepath.ExtractCode(err) != safepath.ErrRolloutFailed {
		t.Fatalf("expected GRABTAC-E601 failure when candidate never becomes ready, got: %v", err)
	}
	if !res.KeptLastKnown || switched || drained {
		t.Fatalf("expected old generation to stay live untouched (kept=%v switched=%v drained=%v)", res.KeptLastKnown, switched, drained)
	}
}

func TestLiveGrabmanAndGrabIntegrationAndAutoRollback(t *testing.T) {
	grabBin := rollout.LocateGrabBinary("")
	grabmanBin := rollout.LocateGrabmanBinary("")
	if grabBin == "" || grabmanBin == "" {
		t.Fatalf("expected live GRAB (%q) and grabman (%q) binaries for ecosystem integration test", grabBin, grabmanBin)
	}

	tmp := t.TempDir()
	dnaOld := filepath.Join(tmp, "site-dna-v1.json")
	dnaNew := filepath.Join(tmp, "site-dna-v2.json")
	writeValidSiteDNA(t, dnaOld, "Urban Roasters", "18.50")
	writeValidSiteDNA(t, dnaNew, "Urban Roasters", "21.00")

	ws := filepath.Join(tmp, "live-shop")
	initCmd := exec.Command(grabmanBin, "init", "--dir", ws, "--profile", "ai-stack", "--name", "Urban Roasters", "--from-site-dna", dnaOld, "--json")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("live grabman init failed: %v\n%s", err, string(out))
	}

	// Add an explicit idempotent GRAB v1.1.0 migration file (ADR-024f)
	migDir := filepath.Join(ws, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}
	migFile := "migrations/001_init.grab"
	migSrc := `use data
set db to data.open using "main"
set migration to data.migrate using db, [record {version: 1, name: "init_rollout_audit", steps: ["CREATE TABLE IF NOT EXISTS rollout_audit (id INTEGER PRIMARY KEY, note TEXT NOT NULL)"]}]
show migration
`
	if err := os.WriteFile(filepath.Join(ws, migFile), []byte(migSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start a live local healthy worker endpoint
	liveWorker := startMockGrispWorker(t, "v1", true)
	defer liveWorker.server.Close()

	// 1. Successful 6-stage rollout with live grabman solve --verify, grabman dna validate, grab check, explicit grab migrate, Site DNA update, and Edge Caddyfile
	res, err := rollout.ExecuteRollout(context.Background(), rollout.RolloutOptions{
		Workspace:        ws,
		AppName:          "urban-roasters",
		GrabBinary:       grabBin,
		GrabmanBinary:    grabmanBin,
		CandidateSiteDNA: dnaNew,
		Migration: &rollout.MigrationConfig{
			MigrationFile: migFile,
			DBConfig:      "grab.data.json",
			DBAlias:       "main",
		},
		EdgeConfig: &container.EdgeRouteConfig{
			EdgeProxy:     "caddy",
			Domain:        "roasters.example.com",
			UpstreamHost:  "127.0.0.1",
			UpstreamPorts: []int{liveWorker.slot.Port},
			HealthPath:    "/_ready",
		},
		SmokeURLs:       []string{liveWorker.slot.BaseURL + "/_ready"},
		RequireLockfile: true,
	})
	if err != nil {
		t.Fatalf("live ExecuteRollout failed: %v", err)
	}
	if !res.Success || !res.Preflight.LockfileVerified || !res.Preflight.GrabCheckPassed || !res.Preflight.SiteDNAValidated {
		t.Fatalf("expected all live preflight & rollout stages to pass: %+v", res)
	}
	dnaAfterBytes, err := os.ReadFile(filepath.Join(ws, "storage", "site-dna.json"))
	if err != nil || !strings.Contains(string(dnaAfterBytes), `"21.00"`) {
		t.Fatalf("expected updated Site DNA price 21.00 after rollout, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "deploy", "Caddyfile")); err != nil {
		t.Fatalf("expected generated deploy/Caddyfile: %v", err)
	}

	// 2. Failing rollout (post-cutover smoke fails after candidate mutates app.grab + Site DNA) -> MUST auto-rollback byte-for-byte!
	origAppBytes, _ := os.ReadFile(filepath.Join(ws, "src", "app.grab"))
	badSmokeServer := startMockGrispWorker(t, "broken", false) // /_ready returns 503
	defer badSmokeServer.server.Close()

	badDNA := filepath.Join(tmp, "site-dna-v3.json")
	writeValidSiteDNA(t, badDNA, "Urban Roasters", "99.95")

	failRes, failErr := rollout.ExecuteRollout(context.Background(), rollout.RolloutOptions{
		Workspace:        ws,
		AppName:          "urban-roasters",
		GrabBinary:       grabBin,
		GrabmanBinary:    grabmanBin,
		CandidateSiteDNA: badDNA,
		ApplyCandidate: func(_ context.Context, targetWS string) error {
			return os.WriteFile(filepath.Join(targetWS, "src", "app.grab"), append(origAppBytes, []byte("\n// candidate v3\n")...), 0o644)
		},
		SmokeURLs:         []string{badSmokeServer.slot.BaseURL + "/_ready"},
		RollbackReadyURLs: []string{liveWorker.slot.BaseURL + "/_ready"},
		RequireLockfile:   true,
	})
	if failErr == nil {
		t.Fatalf("expected rollout failure when smoke /_ready returns 503")
	}
	if !failRes.RolledBack || failRes.RollbackResult == nil || !failRes.RollbackResult.ReadyVerified {
		t.Fatalf("expected automatic Pre-Deploy Snapshot rollback + /_ready verification, got: %+v", failRes)
	}

	restoredAppBytes, _ := os.ReadFile(filepath.Join(ws, "src", "app.grab"))
	if string(restoredAppBytes) != string(origAppBytes) {
		t.Fatalf("auto-rollback did not restore src/app.grab byte-for-byte")
	}
	restoredDNABytes, _ := os.ReadFile(filepath.Join(ws, "storage", "site-dna.json"))
	if !strings.Contains(string(restoredDNABytes), `"21.00"`) || strings.Contains(string(restoredDNABytes), `"99.95"`) {
		t.Fatalf("auto-rollback did not restore storage/site-dna.json to pre-deploy 21.00")
	}

	// 3. Preflight rejection (bad Site DNA with wrapper <div> / float) MUST fail BEFORE creating snapshot or modifying workspace
	unsafeDNA := filepath.Join(tmp, "site-dna-unsafe.json")
	unsafeContent := strings.Replace(string(dnaAfterBytes), `"21.00"`, `21.5`, 1)
	if err := os.WriteFile(unsafeDNA, []byte(unsafeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	preFailRes, preErr := rollout.ExecuteRollout(context.Background(), rollout.RolloutOptions{
		Workspace:        ws,
		AppName:          "urban-roasters",
		GrabBinary:       grabBin,
		GrabmanBinary:    grabmanBin,
		CandidateSiteDNA: unsafeDNA,
		RequireLockfile:  true,
	})
	if preErr == nil || safepath.ExtractCode(preErr) != safepath.ErrPreflightFailed || preFailRes.DeployID != "" {
		t.Fatalf("expected Stage 1 GRABTAC-E101 abort before snapshot creation, got err=%v res=%+v", preErr, preFailRes)
	}
}

func writeValidSiteDNA(t *testing.T, dest, siteName, price string) {
	t.Helper()
	payload := fmt.Sprintf(`{
  "schema_version": "1.0.0",
  "bundle_name": "groxy-site-dna-v1.0.0",
  "exported_at": "2026-11-15T00:00:00Z",
  "read_only_snapshot": true,
  "decimal_integrity_verified": true,
  "compatible_consumers": ["grabman", "grabtaculous", "gashboard"],
  "site_name": %q,
  "site_url": "https://roasters.example.com",
  "active_style_pack": {
    "id": "emerald-editorial",
    "label": "Emerald Editorial",
    "tokens": {
      "light": {
        "primary": "#047857",
        "surface": "#F8FAFC",
        "ink": "#0F172A",
        "accent": "#10B981",
        "muted": "#475569"
      }
    }
  },
  "design_system": {
    "tokens": {
      "primary": "#047857",
      "surface": "#F8FAFC",
      "ink": "#0F172A",
      "accent": "#10B981",
      "muted": "#475569"
    },
    "reusable_classes": [".grex-c-hero"],
    "raw_markdown": "# GRAB-DESIGN-SYSTEM.md"
  },
  "site_context": {
    "products": [
      {
        "title": "House Espresso",
        "path": "/products/espresso",
        "sku": "ESP-01",
        "price": %q,
        "currency": "USD",
        "availability": "in_stock"
      }
    ],
    "redirects": [],
    "counts": {"products": 1}
  },
  "nightly_watch_digest": {
    "site_health_score": 98,
    "overall_grade": "A+"
  }
}`, siteName, price)
	if err := os.WriteFile(dest, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
}
