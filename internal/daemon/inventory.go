package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"grabtaculous/internal/container"
	"grabtaculous/internal/rollout"
	"grabtaculous/internal/safepath"
)

// NodeRecord tracks a managed VPS or local node registered with Grabtaculous.
type NodeRecord struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SSHAlias       string `json:"ssh_alias"`
	EdgeProxy      string `json:"edge_proxy"`
	Status         string `json:"status"`
	PortRangeStart string `json:"port_range_start"`
	PortRangeEnd   string `json:"port_range_end"`
	UpdatedAtUTC   string `json:"updated_at_utc"`
}

// AppRecord tracks an orchestrated application or stack and its loopback worker slots.
type AppRecord struct {
	AppName        string `json:"app_name"`
	BlueprintID    string `json:"blueprint_id,omitempty"`
	Workspace      string `json:"workspace"`
	EdgeProxy      string `json:"edge_proxy"`
	Domain         string `json:"domain"`
	WorkerPorts    []int  `json:"worker_ports"`
	ActiveDeployID string `json:"active_deploy_id,omitempty"`
	ComposeFile    string `json:"compose_file,omitempty"`
	EdgeFile       string `json:"edge_file,omitempty"`
	HealthURL      string `json:"health_url,omitempty"`
	ReadyURL       string `json:"ready_url,omitempty"`
	Status         string `json:"status"`
	UpdatedAtUTC   string `json:"updated_at_utc"`
}

// InventoryDocument is the persisted state/inventory.json file.
type InventoryDocument struct {
	SchemaVersion string                `json:"schema_version"`
	Nodes         map[string]NodeRecord `json:"nodes"`
	Apps          map[string]AppRecord  `json:"apps"`
}

// InventoryStore manages concurrent-safe node and loopback port allocation state.
type InventoryStore struct {
	mu       sync.Mutex
	stateDir string
	now      func() time.Time
}

// NewInventoryStore creates a persistent InventoryStore rooted inside stateDir.
func NewInventoryStore(stateDir string) (*InventoryStore, error) {
	realState, err := safepath.InitRoot(stateDir)
	if err != nil {
		return nil, err
	}
	return &InventoryStore{
		stateDir: realState,
		now:      time.Now,
	}, nil
}

func (s *InventoryStore) loadUnlocked() (*InventoryDocument, error) {
	abs, _, err := safepath.ResolveFile(s.stateDir, "inventory.json", safepath.ResolveOptions{AllowMissing: true})
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		nowStr := s.now().UTC().Format(time.RFC3339)
		return &InventoryDocument{
			SchemaVersion: "0.1.0",
			Nodes: map[string]NodeRecord{
				"local": {
					ID:             "local",
					Name:           "local-supervisor",
					SSHAlias:       "127.0.0.1",
					EdgeProxy:      "caddy",
					Status:         "ready",
					PortRangeStart: strconv.Itoa(rollout.DefaultWorkerBasePort),
					PortRangeEnd:   strconv.Itoa(rollout.DefaultWorkerMaxPort),
					UpdatedAtUTC:   nowStr,
				},
			},
			Apps: map[string]AppRecord{},
		}, nil
	}
	raw, err := safepath.ReadBoundedFile(abs)
	if err != nil {
		return nil, err
	}
	var doc InventoryDocument
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, safepath.WrapError(safepath.ErrControlPlane, fmt.Sprintf("malformed inventory.json: %v", err), "Repair or remove corrupted state/inventory.json.", err)
	}
	if doc.Nodes == nil {
		doc.Nodes = map[string]NodeRecord{}
	}
	if doc.Apps == nil {
		doc.Apps = map[string]AppRecord{}
	}
	return &doc, nil
}

func (s *InventoryStore) saveUnlocked(doc *InventoryDocument) error {
	raw, err := safepath.CanonicalJSON(doc)
	if err != nil {
		return err
	}
	return safepath.WriteFileAtomic(s.stateDir, "inventory.json", raw, 0o640, safepath.ResolveOptions{})
}

// Snapshot returns a consistent copy of the current node & app inventory.
func (s *InventoryStore) Snapshot() (*InventoryDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked()
}

// RegisterNode upserts a managed node while forbidding plaintext secrets and private IP literals in SSH aliases.
func (s *InventoryStore) RegisterNode(node NodeRecord) (*NodeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.TrimSpace(node.ID)
	if id == "" {
		return nil, safepath.NewError(safepath.ErrControlPlane, "node id is required", "Provide a short node identifier such as 'prod-edge-1'.")
	}
	if issues := container.ScanNoPlaintextSecrets("node-registration", node.SSHAlias+" "+node.Name); len(issues) > 0 {
		return nil, safepath.NewError(safepath.ErrSecurityPolicy, strings.Join(issues, " "), "Use an SSH host alias instead of private IP literals or credentials.")
	}
	edge := strings.TrimSpace(node.EdgeProxy)
	if edge == "" {
		edge = "caddy"
	}
	switch edge {
	case "caddy", "nginx", "cloudflare-tunnel":
	default:
		return nil, safepath.NewError(safepath.ErrSecurityPolicy, fmt.Sprintf("unsupported edge_proxy %q", edge), "Use caddy, nginx, or cloudflare-tunnel.")
	}
	if strings.TrimSpace(node.PortRangeStart) == "" {
		node.PortRangeStart = strconv.Itoa(rollout.DefaultWorkerBasePort)
	}
	if strings.TrimSpace(node.PortRangeEnd) == "" {
		node.PortRangeEnd = strconv.Itoa(rollout.DefaultWorkerMaxPort)
	}
	if strings.TrimSpace(node.Status) == "" {
		node.Status = "ready"
	}
	node.EdgeProxy = edge
	node.UpdatedAtUTC = s.now().UTC().Format(time.RFC3339)

	doc, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	doc.Nodes[id] = node
	if err := s.saveUnlocked(doc); err != nil {
		return nil, err
	}
	copyRec := node
	return &copyRec, nil
}

// AllocatePorts reserves `count` loopback ports within 127.0.0.1:8765-8772 for appName.
func (s *InventoryStore) AllocatePorts(appName string, count int) ([]int, error) {
	if count <= 0 {
		count = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}

	if existing, ok := doc.Apps[appName]; ok && len(existing.WorkerPorts) == count {
		out := append([]int(nil), existing.WorkerPorts...)
		return out, nil
	}

	used := map[int]bool{}
	for name, app := range doc.Apps {
		if name == appName {
			continue
		}
		for _, p := range app.WorkerPorts {
			used[p] = true
		}
	}

	var chosen []int
	for p := rollout.DefaultWorkerBasePort; p <= rollout.DefaultWorkerMaxPort && len(chosen) < count; p++ {
		if !used[p] {
			chosen = append(chosen, p)
		}
	}
	if len(chosen) < count {
		return nil, safepath.NewError(
			safepath.ErrControlPlane,
			fmt.Sprintf("insufficient free loopback worker ports in 127.0.0.1:%d-%d for %s (needed %d, found %d)", rollout.DefaultWorkerBasePort, rollout.DefaultWorkerMaxPort, appName, count, len(chosen)),
			"Retire unused apps or reduce worker count.",
		)
	}
	return chosen, nil
}

// UpsertApp stores or updates an application deployment entry in inventory.json.
func (s *InventoryStore) UpsertApp(app AppRecord) (*AppRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := strings.TrimSpace(app.AppName)
	if name == "" {
		return nil, safepath.NewError(safepath.ErrControlPlane, "app_name is required", "Provide a non-empty app name.")
	}
	if app.Workspace != "" {
		app.Workspace = filepath.Clean(app.Workspace)
	}
	app.AppName = name
	app.UpdatedAtUTC = s.now().UTC().Format(time.RFC3339)

	doc, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	doc.Apps[name] = app
	if err := s.saveUnlocked(doc); err != nil {
		return nil, err
	}
	copyRec := app
	return &copyRec, nil
}

// ListApps returns apps sorted alphabetically.
func (s *InventoryStore) ListApps() ([]AppRecord, error) {
	doc, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	out := make([]AppRecord, 0, len(doc.Apps))
	for _, a := range doc.Apps {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AppName < out[j].AppName
	})
	return out, nil
}

// ListNodes returns nodes sorted alphabetically.
func (s *InventoryStore) ListNodes() ([]NodeRecord, error) {
	doc, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	out := make([]NodeRecord, 0, len(doc.Nodes))
	for _, n := range doc.Nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}
