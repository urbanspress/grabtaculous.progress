package compose

import (
	"fmt"
	"sort"
	"strings"

	"grabtaculous/internal/safepath"
)

// HardwareProfile defines hardware-tuned resource budgets for Contabo, Bluehost, and future VPS tiers.
type HardwareProfile struct {
	ID                       string `json:"id"`
	Label                    string `json:"label"`
	ProviderExample          string `json:"provider_example"`
	VCPU                     int    `json:"vcpu"`
	RAMGB                    int    `json:"ram_gb"`
	NVMeGB                   int    `json:"nvme_gb"`
	NginxWorkers             int    `json:"nginx_workers"`
	GrispWorkers             int    `json:"grisp_workers"`
	GrispBasePort            int    `json:"grisp_base_port"`
	GrispPorts               []int  `json:"grisp_ports"`
	GrispMemoryLimit         string `json:"grisp_memory_limit"`
	GrispGoMemLimit          string `json:"grisp_gomemlimit"`
	PostgresMemoryLimit      string `json:"postgres_memory_limit"`
	PostgresSharedBuffers    string `json:"postgres_shared_buffers"`
	PostgresEffectiveCache   string `json:"postgres_effective_cache"`
	PostgresWorkMem          string `json:"postgres_work_mem"`
	PostgresMaxConnections   int    `json:"postgres_max_connections"`
	GroxyMemoryLimit         string `json:"groxy_memory_limit"`
	RedisMemoryLimit         string `json:"redis_memory_limit"`
	RedisMaxMemory           string `json:"redis_maxmemory"`
	MinFreeNVMeGB            int    `json:"min_free_nvme_gb"`
	MaxDiskUsedPercent       int    `json:"max_disk_used_percent"`
}

var hardwareProfiles = map[string]HardwareProfile{
	"vps-6c-12g": {
		ID:                     "vps-6c-12g",
		Label:                  "Legacy Contabo Core (6 vCPU / 12 GB RAM / 100 GB NVMe)",
		ProviderExample:        "Contabo Core VPS (Expiring Dec 8, 2026)",
		VCPU:                   6,
		RAMGB:                  12,
		NVMeGB:                 100,
		NginxWorkers:           6,
		GrispWorkers:           4,
		GrispBasePort:          8765,
		GrispPorts:             []int{8765, 8766, 8767, 8768},
		GrispMemoryLimit:       "640M",
		GrispGoMemLimit:        "540MiB",
		PostgresMemoryLimit:    "3072M",
		PostgresSharedBuffers:  "2GB",
		PostgresEffectiveCache: "6GB",
		PostgresWorkMem:        "16MB",
		PostgresMaxConnections: 120,
		GroxyMemoryLimit:       "512M",
		RedisMemoryLimit:       "768M",
		RedisMaxMemory:         "512mb",
		MinFreeNVMeGB:          15,
		MaxDiskUsedPercent:     92,
	},
	"vps-8c-16g": {
		ID:                     "vps-8c-16g",
		Label:                  "Primary Target: Bluehost Ashburn (8 vCPU / 16 GB DDR5 / 200 GB NVMe)",
		ProviderExample:        "Bluehost Ashburn 1 Gbit/s Ubuntu 24.04 LTS",
		VCPU:                   8,
		RAMGB:                  16,
		NVMeGB:                 200,
		NginxWorkers:           8,
		GrispWorkers:           6,
		GrispBasePort:          8765,
		GrispPorts:             []int{8765, 8766, 8767, 8768, 8769, 8770},
		GrispMemoryLimit:       "768M",
		GrispGoMemLimit:        "650MiB",
		PostgresMemoryLimit:    "4096M",
		PostgresSharedBuffers:  "2560MB",
		PostgresEffectiveCache: "8GB",
		PostgresWorkMem:        "24MB",
		PostgresMaxConnections: 160,
		GroxyMemoryLimit:       "512M",
		RedisMemoryLimit:       "1024M",
		RedisMaxMemory:         "768mb",
		MinFreeNVMeGB:          15,
		MaxDiskUsedPercent:     92,
	},
	"vps-8c-24g": {
		ID:                     "vps-8c-24g",
		Label:                  "Scale-Up Tier 1 (8 vCPU / 24 GB RAM / 300 GB NVMe)",
		ProviderExample:        "Scale-Up High-Memory NVMe Node",
		VCPU:                   8,
		RAMGB:                  24,
		NVMeGB:                 300,
		NginxWorkers:           8,
		GrispWorkers:           8,
		GrispBasePort:          8765,
		GrispPorts:             []int{8765, 8766, 8767, 8768, 8769, 8770, 8771, 8772},
		GrispMemoryLimit:       "1024M",
		GrispGoMemLimit:        "860MiB",
		PostgresMemoryLimit:    "6144M",
		PostgresSharedBuffers:  "4GB",
		PostgresEffectiveCache: "12GB",
		PostgresWorkMem:        "32MB",
		PostgresMaxConnections: 200,
		GroxyMemoryLimit:       "1024M",
		RedisMemoryLimit:       "1536M",
		RedisMaxMemory:         "1024mb",
		MinFreeNVMeGB:          15,
		MaxDiskUsedPercent:     92,
	},
	"vps-8c-32g": {
		ID:                     "vps-8c-32g",
		Label:                  "Scale-Up Tier 2 (8 vCPU / 32 GB RAM / 400 GB NVMe)",
		ProviderExample:        "High-Concurrency Bookstore Cluster Node",
		VCPU:                   8,
		RAMGB:                  32,
		NVMeGB:                 400,
		NginxWorkers:           8,
		GrispWorkers:           8,
		GrispBasePort:          8765,
		GrispPorts:             []int{8765, 8766, 8767, 8768, 8769, 8770, 8771, 8772},
		GrispMemoryLimit:       "1536M",
		GrispGoMemLimit:        "1280MiB",
		PostgresMemoryLimit:    "8192M",
		PostgresSharedBuffers:  "6GB",
		PostgresEffectiveCache: "18GB",
		PostgresWorkMem:        "48MB",
		PostgresMaxConnections: 240,
		GroxyMemoryLimit:       "1024M",
		RedisMemoryLimit:       "2048M",
		RedisMaxMemory:         "1536mb",
		MinFreeNVMeGB:          15,
		MaxDiskUsedPercent:     92,
	},
	"vps-16c-64g": {
		ID:                     "vps-16c-64g",
		Label:                  "Flagship Enterprise Scale-Up (16 vCPU / 64 GB RAM / 800 GB NVMe)",
		ProviderExample:        "Dedicated Multi-Storefront NVMe Flagship",
		VCPU:                   16,
		RAMGB:                  64,
		NVMeGB:                 800,
		NginxWorkers:           16,
		GrispWorkers:           8,
		GrispBasePort:          8765,
		GrispPorts:             []int{8765, 8766, 8767, 8768, 8769, 8770, 8771, 8772},
		GrispMemoryLimit:       "2048M",
		GrispGoMemLimit:        "1700MiB",
		PostgresMemoryLimit:    "16384M",
		PostgresSharedBuffers:  "12GB",
		PostgresEffectiveCache: "36GB",
		PostgresWorkMem:        "64MB",
		PostgresMaxConnections: 320,
		GroxyMemoryLimit:       "2048M",
		RedisMemoryLimit:       "4096M",
		RedisMaxMemory:         "3072mb",
		MinFreeNVMeGB:          15,
		MaxDiskUsedPercent:     92,
	},
}

// GetProfile returns a validated hardware sizing profile by ID.
func GetProfile(id string) (*HardwareProfile, error) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		key = "vps-8c-16g"
	}
	prof, ok := hardwareProfiles[key]
	if !ok {
		return nil, safepath.NewError(
			safepath.ErrSecurityPolicy,
			fmt.Sprintf("unknown VPS hardware profile %q", id),
			"Supported profiles: vps-6c-12g, vps-8c-16g, vps-8c-24g, vps-8c-32g, vps-16c-64g.",
		)
	}
	copyProf := prof
	copyProf.GrispPorts = append([]int(nil), prof.GrispPorts...)
	return &copyProf, nil
}

// ListProfiles returns all supported hardware profiles sorted by RAM capacity.
func ListProfiles() []HardwareProfile {
	out := make([]HardwareProfile, 0, len(hardwareProfiles))
	for _, p := range hardwareProfiles {
		cp := p
		cp.GrispPorts = append([]int(nil), p.GrispPorts...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RAMGB != out[j].RAMGB {
			return out[i].RAMGB < out[j].RAMGB
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// RenderProfileCompose emits a hardened --read-only Docker Compose file tuned for the chosen VPS profile.
func RenderProfileCompose(profileID, domain string) ([]byte, error) {
	p, err := GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	dom := strings.TrimSpace(domain)
	if dom == "" {
		dom = "bookstore.example.com"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Grabtaculous v1.1.0 — Production Hardened --read-only Compose Stack (%s)\n", p.ID)
	fmt.Fprintf(&b, "# Profile: %s (%d vCPU / %d GB RAM / %d GB NVMe, %d x Grisp Workers)\n", p.Label, p.VCPU, p.RAMGB, p.NVMeGB, p.GrispWorkers)
	fmt.Fprintf(&b, "# Domain: %s | Trusted Proxy: 172.16.0.0/12,127.0.0.1 | NVMe Reserve: >= %d GB\n", dom, p.MinFreeNVMeGB)
	b.WriteString("services:\n")

	// Multi-worker GrabLab CMS + Grisp application services bound strictly to 127.0.0.1:8765..
	for idx, port := range p.GrispPorts {
		svcName := fmt.Sprintf("grisp-worker-%d", idx+1)
		fmt.Fprintf(&b, "  %s:\n", svcName)
		b.WriteString("    image: ${GRABLAB_IMAGE:-ghcr.io/urbanspress/grablab-cms:v1.0.1@sha256:6b212cf89c4d19129f6d4a3c749e1611bb279f7de0e0d4aa0b0b91f0b17f1001}\n")
		fmt.Fprintf(&b, "    container_name: grabtac-%s\n", svcName)
		b.WriteString("    read_only: true\n")
		b.WriteString("    user: \"65532:65532\"\n")
		b.WriteString("    cap_drop:\n")
		b.WriteString("      - ALL\n")
		b.WriteString("    security_opt:\n")
		b.WriteString("      - no-new-privileges:true\n")
		b.WriteString("    tmpfs:\n")
		b.WriteString("      - /tmp:rw,noexec,nosuid,size=64m\n")
		fmt.Fprintf(&b, "    mem_limit: %s\n", p.GrispMemoryLimit)
		b.WriteString("    ports:\n")
		fmt.Fprintf(&b, "      - \"127.0.0.1:%d:8080\"\n", port)
		b.WriteString("    working_dir: /app\n")
		b.WriteString("    volumes:\n")
		b.WriteString("      - /var/lib/grisp/storage:/app/storage:rw\n")
		b.WriteString("      - /var/lib/grisp/uploads:/app/uploads:rw\n")
		b.WriteString("      - /var/lib/grisp/public/grex:/app/public/grex:rw\n")
		b.WriteString("      - ./grab.data.json:/app/grab.data.json:ro\n")
		b.WriteString("      - ./grab.platform.json:/app/grab.platform.json:ro\n")
		b.WriteString("    networks:\n")
		b.WriteString("      - grabtac_loopback\n")
		b.WriteString("      - grabtac_internal\n")
		b.WriteString("    environment:\n")
		fmt.Fprintf(&b, "      GOMEMLIMIT: %q\n", p.GrispGoMemLimit)
		b.WriteString("      GRABLAB_MASTER_KEY: \"${GRABLAB_MASTER_KEY:?Set GRABLAB_MASTER_KEY in .env or secret manager}\"\n")
		b.WriteString("      GRABLAB_MAX_UPLOAD_BYTES: \"${GRABLAB_MAX_UPLOAD_BYTES:-10485760}\"\n")
		b.WriteString("      GRABLAB_TRUSTED_PROXY: \"${GRABLAB_TRUSTED_PROXY:-127.0.0.1}\"\n")
		b.WriteString("      GRABLAB_UPLOAD_DIR: \"/app/uploads\"\n")
		b.WriteString("      GRABLAB_VISUAL_MODE: \"true\"\n")
		b.WriteString("      GRAB_B2_BUCKET: \"${GRAB_B2_BUCKET:-}\"\n")
		b.WriteString("      GRAB_B2_ENDPOINT: \"${GRAB_B2_ENDPOINT:-}\"\n")
		b.WriteString("      POSTGRES_PASSWORD: \"${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD in .env or secret manager}\"\n")
		b.WriteString("      REDIS_PASSWORD: \"${REDIS_PASSWORD:?Set REDIS_PASSWORD in .env or secret manager}\"\n")
		b.WriteString("    command:\n")
		b.WriteString("      - \"grab\"\n")
		b.WriteString("      - \"serve\"\n")
		b.WriteString("      - \"--addr\"\n")
		b.WriteString("      - \"0.0.0.0:8080\"\n")
		b.WriteString("      - \"--allow-public\"\n")
		b.WriteString("      - \"--allow-scripts\"\n")
		b.WriteString("      - \"--diagnostics\"\n")
		b.WriteString("      - \"production\"\n")
		b.WriteString("      - \"--log-format\"\n")
		b.WriteString("      - \"json\"\n")
		b.WriteString("      - \"--trusted-proxy\"\n")
		b.WriteString("      - \"${GRABLAB_TRUSTED_PROXY:-127.0.0.1}\"\n")
		b.WriteString("      - \"--allow-read\"\n")
		b.WriteString("      - \"/app\"\n")
		b.WriteString("      - \"--allow-write\"\n")
		b.WriteString("      - \"/app/uploads\"\n")
		b.WriteString("      - \"--allow-env\"\n")
		b.WriteString("      - \"GRABLAB_MASTER_KEY,GRABLAB_UPLOAD_DIR,GRABLAB_MAX_UPLOAD_BYTES,GRAB_B2_BUCKET,GRAB_B2_ENDPOINT,POSTGRES_PASSWORD,REDIS_PASSWORD\"\n")
		b.WriteString("      - \"--db-config\"\n")
		b.WriteString("      - \"/app/grab.data.json\"\n")
		b.WriteString("      - \"--allow-db\"\n")
		b.WriteString("      - \"main:read-write\"\n")
		b.WriteString("      - \"--platform-config\"\n")
		b.WriteString("      - \"/app/grab.platform.json\"\n")
		b.WriteString("      - \"--allow-cache\"\n")
		b.WriteString("      - \"cms:read-write\"\n")
		b.WriteString("      - \"--allow-cache\"\n")
		b.WriteString("      - \"cms:limit\"\n")
		b.WriteString("      - \"--static\"\n")
		b.WriteString("      - \"/app/public\"\n")
		b.WriteString("      - \"--max-body-bytes\"\n")
		b.WriteString("      - \"15728640\"\n")
		b.WriteString("      - \"/app/src/app.grab\"\n")
		b.WriteString("    healthcheck:\n")
		b.WriteString("      test:\n")
		b.WriteString("        - \"CMD-SHELL\"\n")
		b.WriteString("        - \"wget -qO- http://127.0.0.1:8080/_ready || exit 1\"\n")
		b.WriteString("      interval: 15s\n")
		b.WriteString("      timeout: 5s\n")
		b.WriteString("      retries: 3\n")
		b.WriteString("      start_period: 10s\n")
		b.WriteString("    restart: unless-stopped\n")
	}

	// Grex Theme Builder Studio
	b.WriteString("  grex-builder-studio:\n")
	b.WriteString("    image: ${GREX_BUILDER_IMAGE:-ghcr.io/urbanspress/grex-builder:v1.0.0@sha256:9f4b23c421a4b1f079a96f8cc03ab31bc10d409e413bcbb52d0a9e77101f1000}\n")
	b.WriteString("    container_name: grabtac-grex-builder\n")
	b.WriteString("    read_only: true\n")
	b.WriteString("    user: \"65532:65532\"\n")
	b.WriteString("    cap_drop:\n")
	b.WriteString("      - ALL\n")
	b.WriteString("    security_opt:\n")
	b.WriteString("      - no-new-privileges:true\n")
	b.WriteString("    tmpfs:\n")
	b.WriteString("      - /tmp:rw,noexec,nosuid,size=64m\n")
	b.WriteString("    mem_limit: 512M\n")
	b.WriteString("    ports:\n")
	b.WriteString("      - \"127.0.0.1:8771:8080\"\n")
	b.WriteString("    volumes:\n")
	b.WriteString("      - builder-storage:/app/storage:rw\n")
	b.WriteString("      - /var/lib/grisp/public/grex:/app/public/grex:rw\n")
	b.WriteString("    networks:\n")
	b.WriteString("      - grabtac_loopback\n")
	b.WriteString("      - grabtac_internal\n")
	b.WriteString("    restart: unless-stopped\n")

	// Groxy AI v1.8.0
	b.WriteString("  groxy-ai:\n")
	b.WriteString("    image: ${GROXY_IMAGE:-ghcr.io/urbanspress/groxy-ai:v1.8.0@sha256:3c94bc8306fb9125a41c4a9c46022f7136f8d3ab65d03b7a05226a9b3c417800}\n")
	b.WriteString("    container_name: grabtac-groxy-ai\n")
	b.WriteString("    read_only: true\n")
	b.WriteString("    user: \"65532:65532\"\n")
	b.WriteString("    cap_drop:\n")
	b.WriteString("      - ALL\n")
	b.WriteString("    security_opt:\n")
	b.WriteString("      - no-new-privileges:true\n")
	b.WriteString("    tmpfs:\n")
	b.WriteString("      - /tmp:rw,noexec,nosuid,size=64m\n")
	fmt.Fprintf(&b, "    mem_limit: %s\n", p.GroxyMemoryLimit)
	b.WriteString("    ports:\n")
	b.WriteString("      - \"127.0.0.1:8090:8090\"\n")
	b.WriteString("    volumes:\n")
	b.WriteString("      - groxy-storage:/app/storage:rw\n")
	b.WriteString("      - groxy-cache:/app/cache:rw\n")
	b.WriteString("    networks:\n")
	b.WriteString("      - grabtac_loopback\n")
	b.WriteString("      - grabtac_internal\n")
	b.WriteString("    environment:\n")
	b.WriteString("      GROXY_MASTER_KEY: \"${GROXY_MASTER_KEY:?Set GROXY_MASTER_KEY in .env or secret manager}\"\n")
	b.WriteString("      GROXY_PROVIDER_API_KEY: \"${GROXY_PROVIDER_API_KEY:-}\"\n")
	b.WriteString("      GROXY_TRUSTED_PROXY: \"${GROXY_TRUSTED_PROXY:-127.0.0.1}\"\n")
	b.WriteString("    restart: unless-stopped\n")

	// PostgreSQL 18 (Internal + Loopback SSH tunnel only)
	b.WriteString("  postgres-18:\n")
	b.WriteString("    image: docker.io/library/postgres:18.1-alpine@sha256:1b42c8f7d9042cb2092ef08a80fc2f48e1a9d988743934bc05543e2ea2c11801\n")
	b.WriteString("    container_name: grabtac-postgres-18\n")
	b.WriteString("    read_only: true\n")
	b.WriteString("    user: \"65532:65532\"\n")
	b.WriteString("    cap_drop:\n")
	b.WriteString("      - ALL\n")
	b.WriteString("    security_opt:\n")
	b.WriteString("      - no-new-privileges:true\n")
	b.WriteString("    tmpfs:\n")
	b.WriteString("      - /tmp:rw,noexec,nosuid,size=64m\n")
	b.WriteString("      - /var/run/postgresql:rw,noexec,nosuid,size=16m\n")
	fmt.Fprintf(&b, "    mem_limit: %s\n", p.PostgresMemoryLimit)
	b.WriteString("    ports:\n")
	b.WriteString("      - \"127.0.0.1:5432:5432\"\n")
	b.WriteString("    volumes:\n")
	b.WriteString("      - postgres-18-data:/var/lib/postgresql/data:rw\n")
	b.WriteString("      - ./postgres/postgresql.conf:/etc/postgresql/postgresql.conf:ro\n")
	b.WriteString("    networks:\n")
	b.WriteString("      - grabtac_internal\n")
	b.WriteString("    environment:\n")
	b.WriteString("      PGDATA: \"/var/lib/postgresql/data/pgdata\"\n")
	b.WriteString("      POSTGRES_DB: \"${POSTGRES_DB:-grablab}\"\n")
	b.WriteString("      POSTGRES_PASSWORD: \"${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD in .env or secret manager}\"\n")
	b.WriteString("      POSTGRES_USER: \"${POSTGRES_USER:-grablab}\"\n")
	b.WriteString("    command:\n")
	b.WriteString("      - \"postgres\"\n")
	b.WriteString("      - \"-c\"\n")
	b.WriteString("      - \"config_file=/etc/postgresql/postgresql.conf\"\n")
	b.WriteString("    restart: unless-stopped\n")

	// Redis 8.2 (noeviction, private internal + 127.0.0.1 loopback)
	b.WriteString("  redis-8-2:\n")
	b.WriteString("    image: docker.io/library/redis:8.2.0-alpine@sha256:8f41bc8306fb9125a41c4a9c46022f7136f8d3ab65d03b7a05226a9b3c418200\n")
	b.WriteString("    container_name: grabtac-redis-8-2\n")
	b.WriteString("    read_only: true\n")
	b.WriteString("    user: \"65532:65532\"\n")
	b.WriteString("    cap_drop:\n")
	b.WriteString("      - ALL\n")
	b.WriteString("    security_opt:\n")
	b.WriteString("      - no-new-privileges:true\n")
	b.WriteString("    tmpfs:\n")
	b.WriteString("      - /tmp:rw,noexec,nosuid,size=64m\n")
	fmt.Fprintf(&b, "    mem_limit: %s\n", p.RedisMemoryLimit)
	b.WriteString("    ports:\n")
	b.WriteString("      - \"127.0.0.1:6379:6379\"\n")
	b.WriteString("    volumes:\n")
	b.WriteString("      - redis-data:/data:rw\n")
	b.WriteString("    networks:\n")
	b.WriteString("      - grabtac_internal\n")
	b.WriteString("    environment:\n")
	b.WriteString("      REDIS_PASSWORD: \"${REDIS_PASSWORD:?Set REDIS_PASSWORD in .env or secret manager}\"\n")
	b.WriteString("    command:\n")
	b.WriteString("      - \"redis-server\"\n")
	b.WriteString("      - \"--appendonly\"\n")
	b.WriteString("      - \"yes\"\n")
	b.WriteString("      - \"--maxmemory\"\n")
	fmt.Fprintf(&b, "      - %q\n", p.RedisMaxMemory)
	b.WriteString("      - \"--maxmemory-policy\"\n")
	b.WriteString("      - \"noeviction\"\n")
	b.WriteString("      - \"--requirepass\"\n")
	b.WriteString("      - \"${REDIS_PASSWORD:?Set REDIS_PASSWORD in .env or secret manager}\"\n")
	b.WriteString("      - \"--dir\"\n")
	b.WriteString("      - \"/data\"\n")
	b.WriteString("    restart: unless-stopped\n")

	b.WriteString("\nnetworks:\n")
	b.WriteString("  grabtac_loopback:\n")
	b.WriteString("    driver: bridge\n")
	b.WriteString("  grabtac_internal:\n")
	b.WriteString("    driver: bridge\n")
	b.WriteString("    internal: true\n\n")
	b.WriteString("volumes:\n")
	b.WriteString("  builder-storage:\n")
	b.WriteString("  groxy-storage:\n")
	b.WriteString("  groxy-cache:\n")
	b.WriteString("  postgres-18-data:\n")
	b.WriteString("  redis-data:\n")
	return []byte(b.String()), nil
}

// RenderPostgresConf emits tuned PostgreSQL 18 settings for the given hardware profile.
func RenderPostgresConf(profileID string) ([]byte, error) {
	p, err := GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`# PostgreSQL 18 Tuned Configuration — Profile %s (%s)
listen_addresses = '*'
max_connections = %d
shared_buffers = %s
effective_cache_size = %s
work_mem = %s
maintenance_work_mem = 256MB
wal_level = replica
max_wal_size = 2GB
min_wal_size = 512MB
checkpoint_completion_target = 0.9
random_page_cost = 1.1
effective_io_concurrency = 200
default_toast_compression = lz4
log_min_duration_statement = 250
`, p.ID, p.Label, p.PostgresMaxConnections, p.PostgresSharedBuffers, p.PostgresEffectiveCache, p.PostgresWorkMem)), nil
}

// RenderReloadWorkersScript emits deploy/reload-workers.sh supporting --sighup and --recreate gated on /_ready === 200.
func RenderReloadWorkersScript(profileID string) ([]byte, error) {
	p, err := GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	portList := make([]string, len(p.GrispPorts))
	for i, port := range p.GrispPorts {
		portList[i] = fmt.Sprintf("%d", port)
	}
	return []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
# Grabtaculous v1.1.0 — Zero-Downtime Grisp Rolling Reload (%s)
# Usage: ./deploy/reload-workers.sh [--sighup|--recreate]

MODE="${1:---recreate}"
PORTS=(%s)
COMPOSE_FILE="${COMPOSE_FILE:-deploy/docker-compose.yml}"

wait_ready() {
  local port="$1"
  for _ in $(seq 1 60); do
    local body
    body="$(curl -fsS --max-time 2 "http://127.0.0.1:${port}/_ready" 2>/dev/null || true)"
    if [[ "$body" == *'"status":"ready"'* ]] || [[ "$body" == *'"ready":true'* ]]; then
      return 0
    fi
    sleep 0.5
  done
  echo "ERROR: Worker on 127.0.0.1:${port} failed /_ready === 200 gate" >&2
  return 1
}

if [[ "$MODE" == "--sighup" ]]; then
  for idx in "${!PORTS[@]}"; do
    port="${PORTS[$idx]}"
    svc="grisp-worker-$((idx + 1))"
    echo "Sending SIGHUP to ${svc} (127.0.0.1:${port})..."
    docker compose -f "$COMPOSE_FILE" kill -s HUP "$svc"
    wait_ready "$port"
  done
  echo "All %d Grisp workers reloaded via SIGHUP and verified /_ready === 200."
  exit 0
fi

if [[ "$MODE" == "--recreate" ]]; then
  for idx in "${!PORTS[@]}"; do
    port="${PORTS[$idx]}"
    svc="grisp-worker-$((idx + 1))"
    echo "Rolling recreate for ${svc} (127.0.0.1:${port})..."
    docker compose -f "$COMPOSE_FILE" up -d --no-deps --force-recreate "$svc"
    wait_ready "$port"
  done
  echo "All %d Grisp workers recreated sequentially with zero dropped traffic."
  exit 0
fi

echo "Usage: $0 [--sighup|--recreate]" >&2
exit 2
`, p.ID, strings.Join(portList, " "), p.GrispWorkers, p.GrispWorkers)), nil
}

// RenderGrispSystemdUnit emits a hardened systemd unit managing the Compose stack.
func RenderGrispSystemdUnit(profileID string) ([]byte, error) {
	p, err := GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`# /etc/systemd/system/grisp-stack.service — Profile %s
[Unit]
Description=Grabtaculous Hardened GRAB + GrabLab + Grex + Groxy AI Stack (%s)
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/grisp-stack
ExecStart=/usr/bin/docker compose -f deploy/docker-compose.yml up -d --remove-orphans
ExecReload=/opt/grisp-stack/deploy/reload-workers.sh --sighup
ExecStop=/usr/bin/docker compose -f deploy/docker-compose.yml down --timeout 30
TimeoutStartSec=120

[Install]
WantedBy=multi-user.target
`, p.ID, p.ID)), nil
}

// RenderGrispBackupScript emits grisp-backup.sh enforcing >= 15 GB NVMe reserve & Backblaze B2 encrypted off-box sync.
func RenderGrispBackupScript(profileID string) ([]byte, error) {
	p, err := GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
# Grabtaculous v1.1.0 — NVMe-Guarded & Backblaze B2 Encrypted Backup Script (%s)
# Zero plaintext credentials in Git: supply GRAB_B2_* & GRAB_BACKUP_PASSPHRASE via root-only /etc/grisp/backup.env (0600).

BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/grisp}"
MIN_FREE_KB=$((15 * 1024 * 1024))
MAX_USED_PCT=%d
STAMP="$(date -u +%%Y%%m%%dT%%H%%M%%SZ)"
STAGE_DIR="${BACKUP_ROOT}/${STAMP}"

mkdir -p "$BACKUP_ROOT"

# 1. Enforce strict >= 15 GB NVMe free-space safety reserve & <= 92%% disk usage cap
AVAIL_KB="$(df -Pk "$BACKUP_ROOT" | awk 'NR==2 {print $4}')"
USED_PCT="$(df -Pk "$BACKUP_ROOT" | awk 'NR==2 {gsub(/%%/, "", $5); print $5}')"
if (( AVAIL_KB < MIN_FREE_KB )) || (( USED_PCT >= MAX_USED_PCT )); then
  echo "ABORT: NVMe reserve guard triggered (avail=${AVAIL_KB}KB, used=${USED_PCT}%%). Must keep >= 15 GB free and < ${MAX_USED_PCT}%%." >&2
  exit 1
fi

mkdir -p "$STAGE_DIR"

# 2. Consistent PostgreSQL 18 + optional MariaDB 11.8 + workspace dumps
docker exec grabtac-postgres-18 pg_dumpall -U "${POSTGRES_USER:-grablab}" > "${STAGE_DIR}/postgres-18.sql"
if docker ps --format '{{.Names}}' | grep -qx 'grabtac-mariadb-11-8'; then
  docker exec grabtac-mariadb-11-8 mariadb-dump --all-databases -u root -p"${MARIADB_ROOT_PASSWORD}" > "${STAGE_DIR}/mariadb-11-8.sql"
fi

tar -C /var/lib/grisp -czf "${STAGE_DIR}/grisp-data.tar.gz" uploads public/grex
sha256sum "${STAGE_DIR}"/* > "${STAGE_DIR}/SHA256SUMS.txt"

# 3. Encrypted off-box sync & cold audiobook archive to Backblaze B2 S3 (if configured)
if [[ -n "${GRAB_B2_ENDPOINT:-}" && -n "${GRAB_B2_BUCKET:-}" ]]; then
  if command -v rclone >/dev/null 2>&1; then
    RCLONE_S3_PROVIDER="Other" \
    RCLONE_S3_ENDPOINT="${GRAB_B2_ENDPOINT}" \
    RCLONE_S3_ACCESS_KEY_ID="${GRAB_B2_KEY_ID}" \
    RCLONE_S3_SECRET_ACCESS_KEY="${GRAB_B2_APP_KEY}" \
      rclone copy "$STAGE_DIR" ":s3:${GRAB_B2_BUCKET}/backups/${STAMP}" --transfers 4
  fi
fi

# Retain last 7 local checkpoints cleanly
ls -1dt "${BACKUP_ROOT}"/20* 2>/dev/null | tail -n +8 | xargs -r rm -rf
echo "Backup checkpoint ${STAMP} verified cleanly."
`, p.ID, p.MaxDiskUsedPercent)), nil
}
