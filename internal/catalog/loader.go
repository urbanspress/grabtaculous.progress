package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	catalogdata "grabtaculous/catalog"
	"grabtaculous/internal/container"
	"grabtaculous/internal/safepath"
)

// IndexEntry describes one curated blueprint in catalog/index.json.
type IndexEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Version      string `json:"version"`
	Summary      string `json:"summary"`
	ManifestPath string `json:"manifest_path"`
	SHA256       string `json:"sha256"`
}

// CatalogIndex represents catalog/index.json.
type CatalogIndex struct {
	SchemaVersion string       `json:"schema_version"`
	Curated       bool         `json:"curated"`
	Blueprints    []IndexEntry `json:"blueprints"`
}

// RequiredVariable declares an expected runtime environment placeholder.
type RequiredVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Blueprint represents a curated, digest-verified 1-click marketplace application definition.
type Blueprint struct {
	SchemaVersion         string              `json:"schema_version"`
	ID                    string              `json:"id"`
	Name                  string              `json:"name"`
	Category              string              `json:"category"`
	Version               string              `json:"version"`
	Summary               string              `json:"summary"`
	RecommendedEdgeProxy  string              `json:"recommended_edge_proxy"`
	EcosystemLayers       map[string]string   `json:"ecosystem_layers,omitempty"`
	RequiredVariables     []RequiredVariable  `json:"required_variables,omitempty"`
	Stack                 container.StackSpec `json:"stack"`
	VerifiedManifestSHA256 string             `json:"verified_manifest_sha256,omitempty"`
}

// Loader reads and cryptographically verifies curated marketplace blueprints from disk or embedded FS.
type Loader struct {
	CatalogDir string
}

// NewLoader builds a catalog loader; empty catalogDir falls back to embedded curated blueprints.
func NewLoader(catalogDir string) *Loader {
	return &Loader{CatalogDir: strings.TrimSpace(catalogDir)}
}

func (l *Loader) readCatalogBytes(rel string) ([]byte, error) {
	cleanRel := path.Clean(strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/"))
	if cleanRel == "." || strings.HasPrefix(cleanRel, "../") || strings.HasPrefix(cleanRel, "/") {
		return nil, safepath.NewError(safepath.ErrPathConfinement, fmt.Sprintf("invalid catalog path %q", rel), "Use relative catalog manifest paths.")
	}
	if l.CatalogDir != "" {
		root, err := safepath.ResolveRoot(l.CatalogDir)
		if err != nil {
			return nil, err
		}
		abs, _, err := safepath.ResolveFile(root, cleanRel, safepath.ResolveOptions{})
		if err != nil {
			return nil, err
		}
		return safepath.ReadBoundedFile(abs)
	}
	raw, err := fs.ReadFile(catalogdata.FS, cleanRel)
	if err != nil {
		return nil, safepath.WrapError(safepath.ErrCatalogInvalid, fmt.Sprintf("missing embedded catalog file %q", cleanRel), "Verify curated catalog integrity.", err)
	}
	return raw, nil
}

// LoadIndex loads and validates catalog/index.json.
func (l *Loader) LoadIndex() (*CatalogIndex, error) {
	raw, err := l.readCatalogBytes("index.json")
	if err != nil {
		return nil, err
	}
	var idx CatalogIndex
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return nil, safepath.WrapError(safepath.ErrCatalogInvalid, fmt.Sprintf("malformed catalog/index.json: %v", err), "Repair catalog/index.json.", err)
	}
	if !idx.Curated || len(idx.Blueprints) == 0 {
		return nil, safepath.NewError(safepath.ErrCatalogInvalid, "catalog/index.json must declare curated=true and at least one blueprint", "Verify catalog/index.json.")
	}
	return &idx, nil
}

// List verifies and returns all curated catalog entries sorted deterministically.
func (l *Loader) List() ([]IndexEntry, error) {
	idx, err := l.LoadIndex()
	if err != nil {
		return nil, err
	}
	entries := append([]IndexEntry(nil), idx.Blueprints...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

// LoadBlueprint loads, verifies SHA-256 digest against catalog/index.json, and enforces GRABTAC-E401 hardening.
func (l *Loader) LoadBlueprint(id string) (*Blueprint, error) {
	targetID := strings.TrimSpace(id)
	if targetID == "" {
		return nil, safepath.NewError(safepath.ErrCatalogInvalid, "blueprint ID is required", "Run 'grabtaculous catalog list' to view curated blueprints.")
	}
	idx, err := l.LoadIndex()
	if err != nil {
		return nil, err
	}

	var matched *IndexEntry
	for i := range idx.Blueprints {
		if idx.Blueprints[i].ID == targetID {
			matched = &idx.Blueprints[i]
			break
		}
	}
	if matched == nil {
		return nil, safepath.NewError(
			safepath.ErrCatalogInvalid,
			fmt.Sprintf("unknown marketplace blueprint %q (only curated catalog apps are allowed)", targetID),
			"Run 'grabtaculous catalog list' to inspect supported curated blueprints.",
		)
	}

	raw, err := l.readCatalogBytes(matched.ManifestPath)
	if err != nil {
		return nil, err
	}
	actualSHA := safepath.SHA256Bytes(raw)
	if !strings.EqualFold(actualSHA, strings.TrimSpace(matched.SHA256)) {
		return nil, safepath.NewError(
			safepath.ErrCatalogInvalid,
			fmt.Sprintf("SHA-256 digest mismatch for blueprint %q (%s): got %s, expected %s", matched.ID, matched.ManifestPath, actualSHA, matched.SHA256),
			"Restore the signed curated blueprint from the verified release catalog.",
		)
	}

	var bp Blueprint
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&bp); err != nil {
		return nil, safepath.WrapError(safepath.ErrCatalogInvalid, fmt.Sprintf("malformed blueprint %q: %v", matched.ID, err), "Repair blueprint JSON schema.", err)
	}
	if bp.ID != matched.ID {
		return nil, safepath.NewError(safepath.ErrCatalogInvalid, fmt.Sprintf("blueprint id %q does not match index entry %q", bp.ID, matched.ID), "Keep catalog index and blueprint IDs aligned.")
	}

	if err := container.ValidateStackSpec(bp.Stack, container.AuditOptions{RequireDigest: true}); err != nil {
		return nil, err
	}

	bp.VerifiedManifestSHA256 = actualSHA
	return &bp, nil
}

// ValidateVariables ensures caller-supplied variables satisfy required_variables without exposing plaintext defaults in manifests.
func ValidateVariables(bp *Blueprint, supplied map[string]string) error {
	if bp == nil {
		return safepath.NewError(safepath.ErrCatalogInvalid, "blueprint is nil", "Load a valid blueprint first.")
	}
	var missing []string
	for _, rv := range bp.RequiredVariables {
		if !rv.Required {
			continue
		}
		if strings.TrimSpace(supplied[rv.Name]) == "" {
			if _, exists := os.LookupEnv(rv.Name); !exists || strings.TrimSpace(os.Getenv(rv.Name)) == "" {
				missing = append(missing, rv.Name)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return safepath.NewError(
			safepath.ErrSecurityPolicy,
			fmt.Sprintf("missing required runtime variable(s) for blueprint %q: %s", bp.ID, strings.Join(missing, ", ")),
			"Supply required secrets via untracked .env or secret manager environment variables (never hardcode in YAML).",
		)
	}
	return nil
}
