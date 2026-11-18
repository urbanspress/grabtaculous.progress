# GRABTACULOUS-SPEC-110: Grabtaculous `v1.1.0` — NGINX `1.30.6` (HTTP/3 + `X-Accel-Redirect` + Backblaze B2), Multi-Worker Hardware Profiles, Node Migration Engine & SearXNG Blueprint

- **Status:** Complete (Released `v1.1.0` — Backwards-Compatible Minor Feature Release)
- **Target Release:** `v1.1.0` (`MAJOR.MINOR.PATCH` — Additive NGINX `1.30.6` Edge Orchestration, `X-Accel-Redirect` + `aio threads;` + Backblaze B2 Offload, Hardware-Adaptive VPS Profiles, 15 GB NVMe Reserve Guard, Contabo -> Bluehost Node Migration Engine, `vps-starter-kit.md`, & Curated `searxng` / `wordpress-fpm` Companion Blueprints)
- **Foundational Baselines (Frozen `v1.x` Contracts Preserved):**
  - **GRAB:** `v1.1.0` (`urbanspress/grab` — `Grisp` multi-worker supervisor `--workers N --worker-base-port P`, `--trusted-proxy`, `/_health`, `/_ready`, `server.asset` `?v=<12-char-sha256>`, `use sockets` `Origin` checks, `use events` raw-byte HMAC webhooks, `ADR-005`, `ADR-017`, `ADR-024f`, `ADR-025g`, `ADR-027c`)
  - **Grex JS:** `v1.0.0` (`urbanspress/grex-js`)
  - **GrabLab CMS:** `v1.0.1` – `v1.4.0` (`urbanspress/grablab-cms` — `--read-only` compose, `/api/admin/media/upload`, `/api/commerce/download/{token}`, `/api/commerce/webhooks/payment`)
  - **Grex Theme Builder:** `v1.0.0` (`urbanspress/grex-builder`)
  - **Groxy AI:** `v1.0.0` – `v1.8.0` (`urbanspress/groxy-ai` — `Site DNA` `groxy-site-dna-v0.4.0`, private metasearch readiness)
  - **Step 5 Peers:** `grabman v1.0.0`, `gashboard v1.0.0`, `groxy-desk v1.0.0`, `plotific-press`
- **Governance:** Read alongside [`GRABTACULOUS-EXPERT.md`](../GRABTACULOUS-EXPERT.md), [`AGENTS.md`](../AGENTS.md), [`GRABTACULOUS-SPEC-100.md`](GRABTACULOUS-SPEC-100.md), and [`docs/ROADMAP.md`](../docs/ROADMAP.md).

---

## 1. Executive Summary (Plain English)

**Grabtaculous `v1.1.0`** upgrades our production VPS orchestration from single-node starter configs to a battle-tested, high-throughput publishing & digital bookstore stack built for our live migration from **Contabo Core (`vps-6c-12g`, expiring December 8, 2026)** to **Bluehost Ashburn (`vps-8c-16g`, 8 vCPU / 16 GB DDR5 RAM / 200 GB NVMe / 1 Gbit/s Ubuntu 24.04 LTS)** and future scale-up tiers (`vps-8c-24g`, `vps-8c-32g`, `vps-16c-64g`).

Specifically, `v1.1.0` delivers six backwards-compatible capabilities without breaking any frozen `v1.0.0` CLI/daemon contracts:
1. **Official `nginx.org` NGINX `1.30.6` (Stable) Host Bootstrapper (`internal/edge/`):** Safely snapshots `/etc/nginx`, removes stock Ubuntu `nginx 1.24.x` packages and Debian-only `modules-enabled/*.conf` / `snippets/fastcgi-php.conf` includes, pins `nginx.org` PGP keys (`573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62`) for `noble`, applies `/etc/sysctl.d/99-nginx-quic.conf` (`rmem_max`/`wmem_max=7500000`, `somaxconn=8192`), and configures UFW (`443/tcp`, `443/udp` QUIC) via structured `exec.CommandContext` argv slices (zero `sh -c`).
2. **Production-Tuned NGINX `1.30.6` Generator (`nginx.conf`, `grisp.conf`, `wordpress-fpm.conf`):**
   - Enables HTTP/3 (`listen 443 quic reuseport;` once on primary `grisp.conf`, `http2 on;`, `quic_retry on;`, `quic_gso on;`, `ssl_early_data off;`, `Alt-Svc 'h3=":443"; ma=86400'`), `aio threads;`, RAM-only proxy buffering, selective text/JSON-LD Gzip (excluding `.epub`, `.pdf`, `.m4b`, `.mp3`, `.webp`, `.png`, `.woff2`), outer `429` flood shields, WebSocket `Origin` passthrough, and raw-byte webhook passthrough (`/api/webhooks/` & `/api/commerce/webhooks/`).
   - Provides two `internal;` `X-Accel-Redirect` zero-copy delivery endpoints:
     - `/_protected_media/` -> local NVMe `/var/lib/grisp/uploads/` (`sendfile on; aio threads; directio 8m;`)
     - `/_protected_b2/<host>/<path>` -> Backblaze B2 S3-compatible proxy (`proxy_buffering off; proxy_max_temp_file_size 0;` + B2/S3 header stripping) for 300–800 MB audiobooks with zero temp-disk bloat.
   - Emits isolated `wordpress-fpm.conf` (`listen 443 quic;` without duplicate `reuseport`, `fastcgi_params`, `/xmlrpc.php` & `/wp-config.php` & uploads PHP blocks, `wp_login_flood` shield).
3. **Hardware-Adaptive VPS Profiles & 15 GB NVMe Reserve Guard (`internal/compose/`):** Supports `vps-6c-12g`, `vps-8c-16g`, `vps-8c-24g`, `vps-8c-32g`, and `vps-16c-64g` with tuned Compose limits, PostgreSQL 18 `postgresql.conf`, Redis 8.2 `noeviction` bounds, `reload-workers.sh` (`--sighup` and `--recreate` gated on `GET /_ready === 200` + `"status":"ready"`), `grisp-stack.service`, and `grisp-backup.sh` (Backblaze B2 encrypted off-box sync), guarded by a mandatory **>= 15 GB free NVMe / <= 92% disk usage check** (`CheckDiskReserve`).
4. **Deterministic Contabo -> Bluehost Node Migration Engine (`internal/snapshot/migrate.go`):** `grabtaculous migrate-export` and `grabtaculous migrate-import` package SHA-256 verified DB dumps, uploads, `/static/grex/`, `Site DNA`, and `grabman.lock` into tamper-evident bundles (enforcing `safepath` secret exclusion and the 15 GB NVMe reserve guard).
5. **Curated Marketplace Blueprints (`catalog/`):** Preserves the planned `v1.1.0` hardened `--read-only` **SearXNG** private metasearch blueprint (`catalog/companions/searxng.json`) and adds the isolated **WordPress PHP-FPM + MariaDB 11.8** companion blueprint (`catalog/companions/wordpress-fpm.json`).
6. **Master `vps-starter-kit.md` Redeployment & Scale-Up Playbook:** Maintained identically at [`vps-starter-kit.md`](../vps-starter-kit.md) and [`docs/vps-starter-kit.md`](../docs/vps-starter-kit.md).

---

## 2. Cross-Ecosystem Audit Summary (All 10 Repositories)

| Repository | Contract / Version | NGINX `1.30.6` & `X-Accel-Redirect` Compatibility Finding | Action Taken |
|---|---|---|---|
| `GRAB` | `v1.1.0` (Frozen) | `metadata()` parses `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host` behind `--trusted-proxy`. `server.asset` emits `Cache-Control: public, max-age=31536000, immutable` on matching `?v=<12-char-sha256>`. `ServeChannel` enforces single `Origin` + `https` scheme. `AcceptWebhook` verifies raw bytes (`connect.Verify`) and rejects `Content-Encoding != ""`. | None (100% compatible; frozen). |
| `grex-js` | `v1.0.0` (Frozen) | Pure frontend runtime & 20-element library; zero proxy assumptions. | None. |
| `grablab-cms` | `v1.0.1`–`v1.4.0` | `deploy/docker-compose.yml` uses `--read-only`, `127.0.0.1`, `--trusted-proxy`, `--max-body-bytes 15728640`, `--workers`; `deploy/grab.platform.commerce.json` mounts payment webhook at `/api/commerce/webhooks/payment`. | None required in `grablab-cms`; Grabtaculous `grisp.conf` adds `location ^~ /api/commerce/webhooks/` alongside `/api/webhooks/`. |
| `grex-builder` | `v1.0.0` (Frozen) | `deploy/docker-compose.yml` uses `--read-only`, `127.0.0.1`, `--trusted-proxy`. | None. |
| `groxy-ai` | `v1.8.0` (Frozen) | `deploy/docker-compose.yml` uses `--read-only`, `127.0.0.1`, `--trusted-proxy`. Compatible with new `searxng` blueprint. | None. |
| `grabman` | `v1.0.0` (Frozen) | `internal/scaffolder/templates.go` uses `nginx:1.27-alpine` (`>= 1.25.1`, supports `http2 on;`) and contains no legacy `1.24` directives. | None (frozen `v1.0.0` preserved). |
| `gashboard` | `v1.0.0` (Frozen) | Validates `edge_proxy` enum and delegates edge rendering to `grabtaculousd` (`POST /v1/deploy`). | None. |
| `groxy-desk` | `v1.0.0` | `internal/dns/dns.go` (`renderEdgeHTTPSConfig`) emitted deprecated `listen 443 ssl http2;` and omitted `X-Real-IP` / `X-Forwarded-*` headers for NGINX. | Minimal backwards-compatible update in `groxy-desk/internal/dns/dns.go` (`listen 443 ssl;`, `http2 on;`, full `X-Forwarded-*` headers) + unit test verification. |
| `plotific-press` | `v1.0.0` | Publishing playbooks & audit scripts; zero proxy assumptions. | None. |
| `grab-ide-icons` | `v1.0.0` | Icon theme JSON; zero proxy assumptions. | None. |

---

## 3. CLI & API Additions (100% Backwards-Compatible with `v1.0.0`)

- **Version Strings:** `cmd/grabtaculous` `Version = "1.1.0"`, `internal/daemon` `DaemonVersion = "1.1.0"`, `schema_version` remains `"1.0.0"`.
- **New CLI Commands:**
  - `grabtaculous edge <bootstrap-plan|render> [--profile vps-8c-16g] [--domain <domain>] [--wp-domain <domain>] [--workers <n>] [--out-dir <dir>] [--upload-root <dir>] [--json]`
  - `grabtaculous profile <list|render <id>> [--domain <domain>] [--out-dir <dir>] [--json]`
  - `grabtaculous migrate-export [--dir <ws>] [--out <bundle>] [--source-profile vps-6c-12g] [--target-profile vps-8c-16g] [--pg-dump <file>] [--mariadb-dump <file>] [--uploads-dir <dir>] [--grex-dir <dir>] [--json]`
  - `grabtaculous migrate-import --bundle <bundle> [--dir <ws>] [--target-profile vps-8c-16g] [--uploads-dir <dir>] [--grex-dir <dir>] [--verify-only] [--json]`
- **Error Registry Parity:** Preserves `GRABTAC-E101..E701` identically (`GRABTAC-E401` for container/disk/edge policy failures, `GRABTAC-E701` for snapshot/migration bundle & disk reserve failures).
