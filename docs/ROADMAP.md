# Grabtaculous Roadmap

**Repositories:** `urbanspress/grabtaculous.progress` (active development) / `urbanspress/grabtaculous.stable` (verified releases)  
**Foundational Baselines:** **GRAB `v1.1.0`**, **Grex JS `v1.0.0`**, **GrabLab CMS `v1.0.1`**, **Grex Theme Builder `v1.0.0`**, **Groxy AI `v1.0.0`**, and **`grabman v1.0.0`**

**Status Labels:**
- **Specification in Review** — Specification drafted and awaiting explicit project-owner approval before any Go implementation code is written.
- **Approved** — Authorized by the project owner for implementation.
- **Complete** — Implemented, `go test -count=1 ./...` + `govulncheck ./...` + container/rollout/rollback E2E checks passing `100%`, walkthrough PDF generated, and synchronized to `origin` + `stable`.
- **Planned** — Future milestone awaiting prior milestone completion and spec drafting.

---

## `v0.1.0` — Multi-Server Supervisor Daemon, `--read-only` Container Orchestrator, Curated 1-Click Marketplace Catalog, Zero-Downtime `Grisp` Rollouts & Pre-Deploy Snapshot Rollbacks

**Status:** Complete ([`specifications/GRABTACULOUS-SPEC-001.md`](../specifications/GRABTACULOUS-SPEC-001.md) — Released `v0.1.0`, Guide: [`Grabtaculous-v0.1.0-Guide.pdf`](releases/pdf/Grabtaculous-v0.1.0-Guide.pdf))

Approved Scope:
- **Loopback / SSH-Controlled Supervisor Daemon (`grabtaculousd`):** Manages registered VPS nodes and local `Grisp` workers (`127.0.0.1:8765–8772`) over private loopback (`127.0.0.1`) or SSH control channels—zero public panel login port exposed (`ADR-005`).
- **`--read-only` Container & Edge SSL Orchestrator:** Generates and enforces hardened Compose manifests (`read_only: true`, `cap_drop: ["ALL"]`, `no-new-privileges:true`, non-root UID `65532`, loopback port bindings) paired with automatic edge HTTPS (**Caddy** recommended, **Nginx + Certbot**, or **Cloudflare Tunnel**).
- **Curated 1-Click App Marketplace Catalog (`catalog/`):** Pinned, SHA-256-verified templates for **`GrabLab CMS v1.0.1 + Grex Theme Builder v1.0.0`**, **`Groxy AI v1.0.0`**, standalone `Grisp` apps, private Postgres/MariaDB/Redis data stores, and companion self-hosted apps (analytics, object storage, automation, webmail).
- **Zero-Downtime `Grisp` Binary/App Rollouts:** Preflight `grabman solve --verify` + `grab check` validation, explicit pre-rollout `grab migrate`, `/_ready`-gated sequential worker/generation swap (`SIGHUP` / `--reload`, `30s` drain), and automatic last-known-good retention on error.
- **Pre-Deploy Snapshots & Atomic Rollbacks:** Immutable pre-deploy snapshot creation before every change with automatic failback on `/_ready` failure and 1-click `grabtaculous rollback <deploy_id>`.

---

## `v0.2.0` — Fleet Health Streams, Automated Off-Peak Backup Verification & Multi-Node Rolling Canary Gates

**Status:** Planned

Planned Scope:
- Real-time resource & `/_ready` telemetry feed to **Gashboard** over SSH.
- Staging-first canary rollout gating before promoting `Site DNA` or app releases to production VPS pools.

---

## `v1.0.0` — Production Freeze, Hardened Linux Daemon Packages & Full 6-Layer E2E Verification

**Status:** Complete ([`specifications/GRABTACULOUS-SPEC-100.md`](../specifications/GRABTACULOUS-SPEC-100.md) — Released `v1.0.0`, Guide: [`Grabtaculous-v1.0.0-Guide.pdf`](releases/pdf/Grabtaculous-v1.0.0-Guide.pdf))

Completed Scope:
- Completed Step 5 multi-repository cross-layer audit covering loopback-only daemon binding (`127.0.0.1:9440`), constant-time bearer auth, `known_hosts` host-key pinning, `GRAB-E401` (`GRABTAC-E201`) path confinement, zero-shell argv invocation, zero-downtime `Grisp` drain/rollback, and `go test -count=1 ./...` / `govulncheck ./...` (`0` vulnerabilities, [`specifications/GRABTACULOUS-SPEC-100.md`](../specifications/GRABTACULOUS-SPEC-100.md)).
- Froze `v1.0.0` CLI/daemon contracts (`Version = "1.0.0"`, `DaemonVersion = "1.0.0"`, `catalog/index.json` `schema_version = "1.0.0"`), aligned with `grabman v1.0.0`, built static Linux (`amd64`/`arm64`) & Windows (`amd64`) binaries, generated `docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf`, and synchronized `v1.0.0` promotion to `origin` (`urbanspress/grabtaculous.progress`) and `stable` (`urbanspress/grabtaculous.stable`).

---

## `v1.1.0` — NGINX `1.30.6` (HTTP/3 + `X-Accel-Redirect` + Backblaze B2), Multi-Worker Hardware Profiles, 15 GB NVMe Reserve Guard, Node Migration Engine, `vps-starter-kit.md` & Curated SearXNG / WordPress PHP-FPM Blueprints

**Status:** Complete ([`specifications/GRABTACULOUS-SPEC-110.md`](../specifications/GRABTACULOUS-SPEC-110.md) — Released `v1.1.0`, Guide: [`Grabtaculous-v1.1.0-Guide.pdf`](releases/pdf/Grabtaculous-v1.1.0-Guide.pdf), Playbook: [`vps-starter-kit.md`](../vps-starter-kit.md))

Completed Scope:
- **Official `nginx.org` NGINX `1.30.6` (Stable) Host Bootstrapper & Generator (`internal/edge/`):** Zero-shell structured-argv Ubuntu 24.04 (`noble`) upgrade plan stripping legacy `nginx 1.24.x` / `modules-enabled` / `snippets/fastcgi-php.conf` / `listen 443 ssl http2`, pinning PGP key `573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62`, applying `/etc/sysctl.d/99-nginx-quic.conf`, UFW `443/tcp` + `443/udp`, and generating tuned `nginx.conf`, `grisp.conf` (`HTTP/3`, `aio threads;`, `/_protected_media/` + `/_protected_b2/` `X-Accel-Redirect`), and isolated `wordpress-fpm.conf`.
- **Hardware-Adaptive VPS Profiles, 15 GB NVMe Reserve Guard & Backblaze B2 Offload (`internal/compose/`):** Profiles `vps-6c-12g` (Contabo Core), `vps-8c-16g` (Bluehost Ashburn), `vps-8c-24g`, `vps-8c-32g`, and `vps-16c-64g`, `grisp-backup.sh` (Backblaze B2 encrypted off-box sync), `reload-workers.sh` (`--sighup` / `--recreate`), and mandatory `>= 15 GB` free NVMe / `<= 92%` usage checks.
- **Contabo -> Bluehost Node Migration Engine (`internal/snapshot/migrate.go`):** `grabtaculous migrate-export` & `migrate-import` with per-file SHA-256 verification and secret exclusion.
- **Curated Marketplace Blueprints (`catalog/`):** Hardened `--read-only`, loopback-bound (`127.0.0.1`), digest-pinned **SearXNG** private metasearch (`catalog/companions/searxng.json`) and isolated **WordPress PHP-FPM + MariaDB 11.8** (`catalog/companions/wordpress-fpm.json`).
- **Master Playbook:** [`vps-starter-kit.md`](../vps-starter-kit.md) & [`docs/vps-starter-kit.md`](vps-starter-kit.md).

---

## `v1.1.1` — Universal Zero-Shell VPS Hardware Auto-Detection (`--profile auto`), Mandatory 5-Gate Verification Protocol & `grabman doctor --strict` Parity

**Status:** Complete ([`specifications/GRABTACULOUS-SPEC-110.md`](../specifications/GRABTACULOUS-SPEC-110.md) — Released `v1.1.1`, Guide: [`Grabtaculous-v1.1.1-Guide.pdf`](releases/pdf/Grabtaculous-v1.1.1-Guide.pdf), Protocol: [`VPS-DEPLOY-TEST-FIRST.md`](../VPS-DEPLOY-TEST-FIRST.md))

Completed Scope:
- **Zero-Shell Native Hardware Auto-Detection (`internal/compose/detect.go`, `detect_unix.go`, `detect_windows.go`):** Native CPU, physical RAM, and root NVMe disk probing (`grabtaculous profile detect [--json]`, `--profile auto` on `profile render` and `edge render`, plus `--vcpus` / `--ram-mb` / `--disk-gb` / `--free-disk-gb` dry-run flags) enforcing the `>= 15 GB` NVMe reserve guard (`GRABTAC-E701`).
- **Mandatory 5-Gate Verification Protocol (`VPS-DEPLOY-TEST-FIRST.md`):** Documented and verified live across all 5 gates on Contabo (`vps-6c-12g`, 4 workers, NGINX `1.30.6`).
- **StrictJSON & `grabman doctor --strict` Loopback CIDR Polish:** `RenderGrabPlatformJSON` uses loopback-only `"allowed_cidrs": ["127.0.0.1/32", "::1/128"]` with zero unknown fields (`GRAB-E210`), passing `grabman doctor --strict` and `grab check --diagnostics production` with zero warnings or RFC-1918 IP false positives.
- **Reusable Edge Snippets:** Pre-stages `/etc/nginx/conf.d/grisp-locations.inc` and `/etc/nginx/sites-available/grisp-domain.conf.template` cleanly across all 5 hardware tiers.

