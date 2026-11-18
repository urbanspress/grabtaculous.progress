package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grabtaculous/internal/catalog"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
	"grabtaculous/internal/snapshot"
)

const (
	DefaultListenAddr = "127.0.0.1:9440"
	DaemonVersion     = "1.1.0"
)

// ServerConfig configures the loopback / Unix-socket supervisor daemon (ADR-005).
type ServerConfig struct {
	ListenAddr   string
	StateDir     string
	CatalogDir   string
	ControlToken string // optional; generated securely when omitted
}

// Server serves Grabtaculous's authenticated control API strictly on loopback or Unix domain sockets.
type Server struct {
	cfg       ServerConfig
	network   string
	cleanAddr string
	token     string
	store     *InventoryStore
	catalog   *catalog.Loader
	httpSrv   *http.Server
	listener  net.Listener
	boundAddr string
}

// ValidateListenAddress enforces ADR-005 zero public control-plane exposure (GRABTAC-E501).
// Strictly permits 127.0.0.1:<port>, localhost:<port>, [::1]:<port>, or unix:<path>.
func ValidateListenAddress(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = DefaultListenAddr
	}
	if strings.HasPrefix(trimmed, "unix://") || strings.HasPrefix(trimmed, "unix:") {
		sockPath := strings.TrimPrefix(trimmed, "unix://")
		sockPath = strings.TrimPrefix(sockPath, "unix:")
		if strings.TrimSpace(sockPath) == "" || safepath.HasBlockedSegments(sockPath, false) {
			return "", "", safepath.NewError(
				safepath.ErrControlPlane,
				fmt.Sprintf("invalid Unix socket path %q", raw),
				"Provide a clean local socket path free of '..' or sensitive segments.",
			)
		}
		return "unix", filepath.Clean(sockPath), nil
	}

	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "", "", safepath.WrapError(
			safepath.ErrControlPlane,
			fmt.Sprintf("invalid daemon listen address %q (expected 127.0.0.1:<port> or unix:<path>)", raw),
			"Bind strictly to 127.0.0.1:9440 or a rootless Unix socket (ADR-005).",
			err,
		)
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "*" {
		return "", "", safepath.NewError(
			safepath.ErrControlPlane,
			fmt.Sprintf("forbidden public control-plane bind %q (ADR-005 / GRABTAC-E501): grabtaculousd cannot listen on 0.0.0.0 or wildcard addresses", raw),
			"Bind strictly to 127.0.0.1:9440 or a Unix socket and connect Gashboard over an authenticated SSH tunnel.",
		)
	}
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", "", safepath.NewError(
				safepath.ErrControlPlane,
				fmt.Sprintf("forbidden non-loopback control-plane address %q (ADR-005 / GRABTAC-E501)", raw),
				"Use 127.0.0.1:<port>, [::1]:<port>, or unix:<path> exclusively.",
			)
		}
	}
	if port == "" {
		return "", "", safepath.NewError(safepath.ErrControlPlane, "listen port is required", "Specify 127.0.0.1:9440.")
	}
	return "tcp", net.JoinHostPort(host, port), nil
}

// NewServer validates addresses, initializes state & token security, and builds the control server.
func NewServer(cfg ServerConfig) (*Server, error) {
	network, cleanAddr, err := ValidateListenAddress(cfg.ListenAddr)
	if err != nil {
		return nil, err
	}
	stateDir := strings.TrimSpace(cfg.StateDir)
	if stateDir == "" {
		stateDir = ".grabtaculous"
	}
	store, err := NewInventoryStore(stateDir)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.ControlToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GRABTACULOUS_CONTROL_TOKEN"))
	}
	if token == "" {
		var rnd [32]byte
		if _, rErr := rand.Read(rnd[:]); rErr != nil {
			return nil, rErr
		}
		token = hex.EncodeToString(rnd[:])
	}
	if err := safepath.WriteFileAtomic(store.stateDir, "control.token", []byte(token+"\n"), 0o600, safepath.ResolveOptions{}); err != nil {
		return nil, err
	}

	s := &Server{
		cfg:       cfg,
		network:   network,
		cleanAddr: cleanAddr,
		token:     token,
		store:     store,
		catalog:   catalog.NewLoader(cfg.CatalogDir),
	}
	s.httpSrv = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return s, nil
}

// BoundAddress returns the active listener address once Start is running.
func (s *Server) BoundAddress() string {
	return s.boundAddr
}

// ControlToken returns the active bearer token for local CLI / SSH-tunneled Gashboard clients.
func (s *Server) ControlToken() string {
	return s.token
}

// Start binds the loopback or Unix socket listener and serves requests until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s.network == "unix" {
		if err := os.MkdirAll(filepath.Dir(s.cleanAddr), 0o750); err != nil {
			return err
		}
		_ = os.Remove(s.cleanAddr)
	}
	ln, err := net.Listen(s.network, s.cleanAddr)
	if err != nil {
		return safepath.WrapError(safepath.ErrControlPlane, fmt.Sprintf("failed to bind %s %s: %v", s.network, s.cleanAddr, err), "Check if port is already in use.", err)
	}
	s.listener = ln
	s.boundAddr = ln.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		if srvErr := s.httpSrv.Serve(ln); srvErr != nil && !errors.Is(srvErr, http.ErrServerClosed) {
			errCh <- srvErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
		return <-errCh
	case srvErr := <-errCh:
		return srvErr
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_health", s.handleHealth)
	mux.HandleFunc("GET /_ready", s.handleReady)
	mux.HandleFunc("GET /v1/nodes", s.requireAuth(s.handleGetNodes))
	mux.HandleFunc("POST /v1/nodes", s.requireAuth(s.handlePostNode))
	mux.HandleFunc("GET /v1/catalog", s.requireAuth(s.handleCatalog))
	mux.HandleFunc("POST /v1/deploy", s.requireAuth(s.handleDeploy))
	mux.HandleFunc("POST /v1/rollout", s.requireAuth(s.handleRollout))
	mux.HandleFunc("POST /v1/rollback", s.requireAuth(s.handleRollback))
	mux.HandleFunc("GET /v1/snapshots", s.requireAuth(s.handleSnapshots))
	mux.HandleFunc("GET /v1/status", s.requireAuth(s.handleStatus))
	return mux
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeHTTPJSON(w, http.StatusUnauthorized, map[string]any{
				"ok":    false,
				"code":  string(safepath.ErrControlPlane),
				"error": "missing Bearer control token (ADR-005 authenticated loopback/SSH control plane)",
			})
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeHTTPJSON(w, http.StatusUnauthorized, map[string]any{
				"ok":    false,
				"code":  string(safepath.ErrControlPlane),
				"error": "invalid control token",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "grabtaculousd",
		"version": DaemonVersion,
		"bind":    s.boundAddr,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if _, err := s.catalog.LoadIndex(); err != nil {
		writeHTTPJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "curated catalog index unavailable",
		})
		return
	}
	if _, err := s.store.Snapshot(); err != nil {
		writeHTTPJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "inventory store unavailable",
		})
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"ready":   true,
		"service": "grabtaculousd",
		"version": DaemonVersion,
	})
}

func (s *Server) handleGetNodes(w http.ResponseWriter, _ *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	apps, err := s.store.ListApps()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"nodes": nodes,
		"apps":  apps,
	})
}

func (s *Server) handlePostNode(w http.ResponseWriter, r *http.Request) {
	var req NodeRecord
	if err := decodeJSONBody(r, &req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.store.RegisterNode(req)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"node": saved,
	})
}

func (s *Server) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	items, err := s.catalog.List()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"blueprints": items,
	})
}

type deployRequest struct {
	BlueprintID    string `json:"blueprint_id"`
	Workspace      string `json:"workspace"`
	AppName        string `json:"app_name"`
	Domain         string `json:"domain"`
	EdgeProxy      string `json:"edge_proxy"`
	Workers        int    `json:"workers"`
	SkipLockVerify bool   `json:"skip_lock_verify"`
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	res, err := DeployBlueprint(r.Context(), DeployOptions{
		BlueprintID:    req.BlueprintID,
		TargetDir:      req.Workspace,
		StateDir:       s.store.stateDir,
		CatalogDir:     s.cfg.CatalogDir,
		AppName:        req.AppName,
		Domain:         req.Domain,
		EdgeProxy:      req.EdgeProxy,
		Workers:        req.Workers,
		SkipLockVerify: req.SkipLockVerify,
	})
	if err != nil {
		writeHTTPError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"deploy": res,
	})
}

type rolloutRequest struct {
	Workspace        string `json:"workspace"`
	AppName          string `json:"app_name"`
	CandidateSiteDNA string `json:"candidate_site_dna"`
	MigrationFile    string `json:"migration_file"`
	DBConfig         string `json:"db_config"`
	DBAlias          string `json:"db_alias"`
	RequireLockfile  bool   `json:"require_lockfile"`
	SkipGrabCheck    bool   `json:"skip_grab_check"`
}

func (s *Server) handleRollout(w http.ResponseWriter, r *http.Request) {
	var req rolloutRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	var mig *rollout.MigrationConfig
	if strings.TrimSpace(req.MigrationFile) != "" {
		mig = &rollout.MigrationConfig{
			MigrationFile: req.MigrationFile,
			DBConfig:      req.DBConfig,
			DBAlias:       req.DBAlias,
		}
	}
	res, err := rollout.ExecuteRollout(r.Context(), rollout.RolloutOptions{
		Workspace:        req.Workspace,
		StateDir:         s.store.stateDir,
		AppName:          req.AppName,
		CandidateSiteDNA: req.CandidateSiteDNA,
		Migration:        mig,
		RequireLockfile:  req.RequireLockfile,
		SkipGrabCheck:    req.SkipGrabCheck,
	})
	if err != nil {
		writeHTTPJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"ok":      false,
			"code":    string(safepath.ExtractCode(err)),
			"error":   err.Error(),
			"rollout": res,
		})
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"rollout": res,
	})
}

type rollbackRequest struct {
	DeployID  string   `json:"deploy_id"`
	Workspace string   `json:"workspace"`
	ReadyURLs []string `json:"ready_urls"`
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req rollbackRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	res, err := snapshot.Rollback(r.Context(), snapshot.RollbackOptions{
		StateDir:  s.store.stateDir,
		Workspace: req.Workspace,
		DeployID:  req.DeployID,
		ReadyURLs: req.ReadyURLs,
	})
	if err != nil {
		writeHTTPError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"rollback": res,
	})
}

func (s *Server) handleSnapshots(w http.ResponseWriter, _ *http.Request) {
	items, err := snapshot.List(s.store.stateDir)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"snapshots": items,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.store.Snapshot()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	snaps, _ := snapshot.List(s.store.stateDir)
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "grabtaculousd",
		"version":        DaemonVersion,
		"bind":           s.boundAddr,
		"node_count":     len(doc.Nodes),
		"app_count":      len(doc.Apps),
		"snapshot_count": len(snaps),
		"inventory":      doc,
	})
}

func decodeJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return safepath.WrapError(safepath.ErrControlPlane, fmt.Sprintf("invalid JSON request: %v", err), "Check JSON payload fields.", err)
	}
	return nil
}

func writeHTTPError(w http.ResponseWriter, status int, err error) {
	code := string(safepath.ExtractCode(err))
	payload := map[string]any{
		"ok":    false,
		"error": err.Error(),
	}
	if code != "" {
		payload["code"] = code
	}
	writeHTTPJSON(w, status, payload)
}

func writeHTTPJSON(w http.ResponseWriter, status int, payload any) {
	raw, err := safepath.CanonicalJSON(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":"internal JSON encoding error"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
