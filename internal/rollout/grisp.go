package rollout

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"grabtaculous/internal/safepath"
)

const (
	DefaultWorkerBasePort = 8765
	DefaultWorkerMaxPort  = 8772
	DefaultDrainTimeout   = 30 * time.Second
	DefaultReadyTimeout   = 15 * time.Second
)

// WorkerSlot identifies a single loopback Grisp worker endpoint (127.0.0.1:8765-8772).
type WorkerSlot struct {
	Port       int    `json:"port"`
	BaseURL    string `json:"base_url"`
	Generation string `json:"generation"`
	PID        int    `json:"pid,omitempty"`
}

// NewLoopbackWorker builds a validated loopback WorkerSlot for 127.0.0.1:<port>.
func NewLoopbackWorker(port int, generation string) (WorkerSlot, error) {
	if port < 1 || port > 65535 {
		return WorkerSlot{}, safepath.NewError(safepath.ErrControlPlane, fmt.Sprintf("invalid worker port %d", port), "Use valid ports in 127.0.0.1:8765-8772.")
	}
	return WorkerSlot{
		Port:       port,
		BaseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		Generation: generation,
	}, nil
}

// ValidateLoopbackURL ensures a worker or health target never points off-host for control probes.
func ValidateLoopbackURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return safepath.WrapError(safepath.ErrControlPlane, fmt.Sprintf("invalid URL %q", rawURL), "Provide a valid http://127.0.0.1:<port> URL.", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return safepath.NewError(safepath.ErrControlPlane, fmt.Sprintf("unsupported probe scheme %q", u.Scheme), "Use http://127.0.0.1:<port>.")
	}
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return safepath.NewError(
			safepath.ErrControlPlane,
			fmt.Sprintf("worker URL %q must target loopback (127.0.0.1, localhost, or ::1)", rawURL),
			"Keep Grisp workers strictly bound to loopback behind the edge TLS proxy.",
		)
	}
	return nil
}

// ProbeHealthAndReady checks both GET /_health === 200 and GET /_ready === 200 on baseURL.
func ProbeHealthAndReady(ctx context.Context, client *http.Client, baseURL string) error {
	if err := ValidateLoopbackURL(baseURL); err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	trimmed := strings.TrimRight(baseURL, "/")
	for _, ep := range []string{"/_health", "/_ready"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed+ep, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s unreachable: %w", ep, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s returned HTTP %d (expected 200)", ep, resp.StatusCode)
		}
	}
	return nil
}

// WaitReady polls /_health and /_ready until both return HTTP 200 or ReadyTimeout expires.
func WaitReady(ctx context.Context, baseURL string, timeout, pollInterval time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	if pollInterval <= 0 {
		pollInterval = 25 * time.Millisecond
	}
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	var lastErr error
	for {
		if err := ProbeHealthAndReady(ctx, client, baseURL); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("readiness wait cancelled for %s", baseURL), "Check worker status.", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
	return safepath.NewError(
		safepath.ErrRolloutFailed,
		fmt.Sprintf("worker %s failed /_ready === 200 gate within %s (%v)", baseURL, timeout, lastErr),
		"Candidate generation aborted before draining old workers; inspect worker logs.",
	)
}

// RollingSwapConfig drives zero-downtime sequential generation replacement across Grisp workers.
type RollingSwapConfig struct {
	OldWorkers       []WorkerSlot
	NewWorkers       []WorkerSlot
	ReadyTimeout     time.Duration
	DrainTimeout     time.Duration
	PollInterval     time.Duration
	AssetParityPaths []string
	StartCandidate   func(ctx context.Context, slot WorkerSlot) error
	SwitchUpstream   func(ctx context.Context, active []WorkerSlot) error
	DrainAndStop     func(ctx context.Context, slot WorkerSlot, drainTimeout time.Duration) error
}

// RollingSwapResult reports the outcome of a RollingSwap execution.
type RollingSwapResult struct {
	PromotedWorkers  []WorkerSlot `json:"promoted_workers"`
	DrainedWorkers   []WorkerSlot `json:"drained_workers"`
	KeptLastKnown    bool         `json:"kept_last_known"`
	AssetParityCheck bool         `json:"asset_parity_check"`
}

// RollingSwap starts and verifies all NewWorkers on /_health and /_ready === 200 (plus optional
// server.asset parity paths) BEFORE flipping the edge upstream and draining OldWorkers.
// If any candidate fails readiness, candidates are stopped and OldWorkers remain untouched.
func RollingSwap(ctx context.Context, cfg RollingSwapConfig) (*RollingSwapResult, error) {
	if len(cfg.NewWorkers) == 0 {
		return nil, safepath.NewError(safepath.ErrRolloutFailed, "rolling swap requires at least one candidate worker", "Allocate loopback ports in 127.0.0.1:8765-8772.")
	}
	for _, w := range cfg.NewWorkers {
		if err := ValidateLoopbackURL(w.BaseURL); err != nil {
			return nil, err
		}
	}
	for _, w := range cfg.OldWorkers {
		if err := ValidateLoopbackURL(w.BaseURL); err != nil {
			return nil, err
		}
	}

	readyTimeout := cfg.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = DefaultReadyTimeout
	}
	drainTimeout := cfg.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = DefaultDrainTimeout
	}

	var startedCandidates []WorkerSlot
	abortCandidates := func() {
		if cfg.DrainAndStop == nil {
			return
		}
		for i := len(startedCandidates) - 1; i >= 0; i-- {
			_ = cfg.DrainAndStop(context.Background(), startedCandidates[i], 2*time.Second)
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	for _, cand := range cfg.NewWorkers {
		if cfg.StartCandidate != nil {
			if err := cfg.StartCandidate(ctx, cand); err != nil {
				abortCandidates()
				return &RollingSwapResult{KeptLastKnown: true}, safepath.WrapError(
					safepath.ErrRolloutFailed,
					fmt.Sprintf("failed starting candidate worker %s: %v", cand.BaseURL, err),
					"Last-known-good workers remain active and serving traffic.",
					err,
				)
			}
		}
		startedCandidates = append(startedCandidates, cand)

		if err := WaitReady(ctx, cand.BaseURL, readyTimeout, cfg.PollInterval); err != nil {
			abortCandidates()
			return &RollingSwapResult{KeptLastKnown: true}, err
		}

		for _, assetPath := range cfg.AssetParityPaths {
			target := strings.TrimRight(cand.BaseURL, "/") + "/" + strings.TrimLeft(assetPath, "/")
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if reqErr != nil {
				abortCandidates()
				return &RollingSwapResult{KeptLastKnown: true}, reqErr
			}
			resp, doErr := client.Do(req)
			if doErr == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
			}
			if doErr != nil || resp.StatusCode != http.StatusOK {
				abortCandidates()
				return &RollingSwapResult{KeptLastKnown: true}, safepath.NewError(
					safepath.ErrRolloutFailed,
					fmt.Sprintf("ADR-027b asset parity check failed on %s for %s", cand.BaseURL, assetPath),
					"Ensure server.asset ?v=<hash> and /static/grex/ bundles are mounted cleanly on the new generation before cutover.",
				)
			}
		}
	}

	if cfg.SwitchUpstream != nil {
		if err := cfg.SwitchUpstream(ctx, cfg.NewWorkers); err != nil {
			abortCandidates()
			return &RollingSwapResult{KeptLastKnown: true}, safepath.WrapError(
				safepath.ErrRolloutFailed,
				fmt.Sprintf("upstream switch failed: %v", err),
				"Last-known-good upstream remains active.",
				err,
			)
		}
	}

	var drained []WorkerSlot
	if cfg.DrainAndStop != nil {
		for _, old := range cfg.OldWorkers {
			if err := cfg.DrainAndStop(ctx, old, drainTimeout); err != nil {
				return nil, safepath.WrapError(
					safepath.ErrRolloutFailed,
					fmt.Sprintf("old worker %s drain failed: %v", old.BaseURL, err),
					"Verify old worker exited cleanly after 30s drain window.",
					err,
				)
			}
			drained = append(drained, old)
		}
	}

	return &RollingSwapResult{
		PromotedWorkers:  cfg.NewWorkers,
		DrainedWorkers:   drained,
		KeptLastKnown:    false,
		AssetParityCheck: len(cfg.AssetParityPaths) > 0,
	}, nil
}

// ReloadConfig configures an in-place PreparedApp generation reload (SIGHUP or custom reload hook).
type ReloadConfig struct {
	BaseURL      string
	PID          int
	ReadyTimeout time.Duration
	PollInterval time.Duration
	TriggerHook  func(ctx context.Context) error
}

// ReloadGeneration triggers a PreparedApp reload and verifies /_health + /_ready === 200.
func ReloadGeneration(ctx context.Context, cfg ReloadConfig) error {
	if err := ValidateLoopbackURL(cfg.BaseURL); err != nil {
		return err
	}
	if cfg.TriggerHook != nil {
		if err := cfg.TriggerHook(ctx); err != nil {
			return safepath.WrapError(safepath.ErrRolloutFailed, fmt.Sprintf("reload trigger failed: %v", err), "Last-known-good generation remains running.", err)
		}
	} else if cfg.PID > 0 {
		if err := SendReloadSignal(cfg.PID); err != nil {
			return safepath.WrapError(safepath.ErrRolloutFailed, err.Error(), "Use rolling worker swap or a reload hook on Windows.", err)
		}
	}
	return WaitReady(ctx, cfg.BaseURL, cfg.ReadyTimeout, cfg.PollInterval)
}
