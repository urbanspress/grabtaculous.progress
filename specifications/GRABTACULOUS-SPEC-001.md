# GRABTACULOUS-SPEC-001: Grabtaculous `v0.1.0` Multi-Server Supervisor Daemon, `--read-only` Container Orchestrator, 1-Click Marketplace, Zero-Downtime `Grisp` Rollouts & Snapshot Rollbacks

- **Status:** Approved — Complete (Implemented, Verified 100%, and Released as `v0.1.0`)
- **Milestone:** `v0.1.0` — Step 5 Orchestration & Marketplace Engine
- **Foundational Upstream & Peer Baselines:**
  - **GRAB:** `v1.1.0` (`urbanspress/grab` — `Grisp` persistent multi-worker server `--workers N --worker-base-port P`, `--reload` / `SIGHUP` / `SIGTERM`, `/_health`, `/_ready`, `server.asset`, `grab migrate`, `ADR-005`, `ADR-011`, `ADR-017`, `ADR-024f`, `ADR-025g`, `ADR-027b`)
  - **GrabLab CMS:** `v1.0.1` (`deploy/docker-compose.yml` `--read-only` contract, exclusive active theme mounting)
  - **Grex Theme Builder:** `v1.0.0` (`deploy/docker-compose.yml`, `/app/public/grex`, `GRAB-DESIGN-SYSTEM.md`)
  - **Groxy AI:** `v1.0.0` (`deploy/docker-compose.yml` `ecosystem-stack`, portable `Site DNA` `groxy-site-dna-v0.4.0`)
  - **`grabman`:** `v0.1.0` (`grabman.lock`, `grabman solve --verify`, `grabman doctor`, `grabman dna`)
- **Governance:** Read alongside [`GRABTACULOUS-EXPERT.md`](../GRABTACULOUS-EXPERT.md), [`AGENTS.md`](../AGENTS.md), and [`docs/ROADMAP.md`](../docs/ROADMAP.md).

---

## 1. Purpose & `v0.1.0` Scope

Traditional VPS control panels leave a public web admin port exposed to internet scanners and run bloated root-level scripts on every server. **Grabtaculous `v0.1.0`** replaces that entire attack surface with a lightweight, unprivileged Go daemon (`grabtaculousd` + CLI, `go 1.26.0`, `CGO_ENABLED=0`) that binds **strictly to private loopback (`127.0.0.1`) or SSH control channels**, enforces `--read-only` container isolation, supplies a curated Softaculous-style **1-Click App Marketplace**, coordinates zero-downtime `Grisp` rolling upgrades, and protects every rollout with automatic Pre-Deploy Snapshot Rollbacks.

### Included in `v0.1.0`
1. **Multi-Server Supervisor Daemon (`grabtaculousd`) & Loopback Control RPC:**
   - Runs quietly on each managed server bound exclusively to `127.0.0.1:9440` (or Unix domain socket) and responds to authenticated **Gashboard** commands over encrypted SSH tunnels—zero public panel login port (`0.0.0.0` strictly forbidden).
   - Tracks managed nodes, deployed stacks, `Grisp` worker pools (`127.0.0.1:8765–8772`), `/_health` & `/_ready` states, resource budgets, and Caddy/Nginx/Cloudflare edge routing rules.
2. **`--read-only` Container Orchestrator & Edge SSL Wiring (`ADR-005`, `ADR-017`):**
   - Renders, validates, and applies hardened Compose/OCI deployment plans enforcing:
     - `read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, non-root UID (`65532:65532`), `tmpfs: ["/tmp:rw,noexec,nosuid,size=64m"]`
     - Loopback port publishing (`127.0.0.1:<port>:<port>`) behind an automated edge TLS proxy (**Caddy** default recommended for zero-cron Let's Encrypt/ZeroSSL + on-demand custom-domain SSL, **Nginx + Certbot**, or **Cloudflare Tunnel**) passing `--trusted-proxy 127.0.0.1`
     - Private internal networking for PostgreSQL 18, MariaDB 11.8, and standalone Redis (never exposed publicly—accessible externally only via **Gashboard** SSH tunnels)
     - Fail-closed admission policy (`GRABTAC-E401`) rejecting `:latest` tags, privileged flags, `host` network mode, Docker socket mounts inside app containers, and committed plaintext secrets.
3. **Curated 1-Click App Marketplace Catalog (`catalog/`):**
   - Pinned, digest-verified JSON/Compose app definitions (zero remote `curl | sh` scripts, zero third-party code plugins):
     - **Primary Ecosystem Stacks:**
       - `grablab-grex-bundle`: **GrabLab CMS `v1.0.1` + Grex Theme Builder `v1.0.0`** pre-wired with exclusive `mode: "visual"` theme ownership and shared `/static/grex/` static asset mount (`ADR-027b`).
       - `grablab-full-stack`: **GrabLab CMS + Grex Theme Builder + Groxy AI `v1.0.0`** (`ecosystem-stack` profile with `--allow-net` strictly scoped to internal services).
       - `grab-grisp-app`: Standalone **`GRAB v1.1.0`** `Grisp` multi-worker application stack.
     - **Curated Data & Companion Stacks:**
       - `postgres-18`, `mariadb-11-8`, `redis-standalone` (loopback/internal-net only), plus curated self-hosted companion apps (`analytics`, `object-storage`, `automation`, `webmail`) with pinned OCI versions and non-root security templates.
4. **Zero-Downtime `Grisp` Binary & Application Rollouts (`ADR-024f`, `ADR-025g`, `ADR-027b`):**
   - Runs a strict 6-stage state machine before cutting over live visitor traffic:
     1. **Lockfile & Static Preflight:** Runs `grabman solve --verify` and `grab check` against candidate artifacts (`GRABTAC-E101` on failure; live app untouched).
     2. **Pre-Deploy Snapshot:** Captures an immutable Pre-Deploy Snapshot before any file, DB, or container change (`snapshots/<deploy_id>/`).
     3. **Explicit Maintenance Migrations (`ADR-024f`):** Runs `grab migrate` explicitly only when declared in the deployment plan—never hidden inside worker HTTP reloads.
     4. **Rolling / Reload Cutover:**
        - *Config/App/Site DNA reload:* Triggers atomic `PreparedApp` reload (`SIGHUP` on Linux / `--reload` or supervised generation switch), preserving last-known-good apps automatically if candidate load fails.
        - *Multi-worker / Grisp binary upgrade:* Drains and cycles workers sequentially across `127.0.0.1:8765–8772` (max `30s` drain timeout per `GRAB-SCALE-SPEC-008`), gating edge proxy upstream activation strictly on `GET /_ready === 200` and preserving `server.asset` `?v=<hash>` compatibility across rolling generations (`ADR-027b`).
     5. **Post-Cutover Smoke Verification:** Polls `/_health`, `/_ready`, and optional configured smoke URLs.
     6. **Automatic Failback:** Immediately triggers Snapshot Rollback if any `/_ready` or smoke gate fails.
5. **Pre-Deploy Snapshots & Atomic Rollbacks (`grabtaculous snapshot` & `grabtaculous rollback`):**
   - Captures immutable snapshots (`grabman.lock`, Compose/Caddy manifests, `public/grex/` static bundle, active `Site DNA` bundle, and pre-deploy database checkpoint hooks for SQLite/PostgreSQL/MariaDB) indexed by `deploy_id` with SHA-256 verification.
   - Restores prior containers, edge proxy routes, static files, and `Site DNA` state atomically on `grabtaculous rollback <deploy_id>` (or automatic failure trigger).

### Explicitly Deferred to `v0.2.0+`
- Multi-region Raft clustering / Kubernetes CRD controller.
- Remote GPU Ollama cluster scheduler.
- Multi-tenant billing / reseller web portals.

---

## 2. Planned `v0.1.0` Repository Layout (`C:\Development\grabtaculous`)

```text
grabtaculous/
├── GRABTACULOUS-EXPERT.md
├── AGENTS.md
├── HANDOFF.md
├── README.md
├── LICENSE
├── .gitignore
├── go.mod                                  # module grabtaculous (go 1.26.0)
├── cmd/
│   └── grabtaculous/
│       └── main.go                         # Daemon & CLI entrypoint: daemon, catalog, deploy, rollout, rollback, snapshot, status
├── catalog/
│   ├── index.json                          # Signed/hashed index of curated 1-click apps
│   ├── grablab-grex-bundle.json            # GrabLab CMS v1.0.1 + Grex Theme Builder v1.0.0 blueprint
│   ├── grablab-full-stack.json             # GrabLab + Grex Builder + Groxy AI v1.0.0 stack
│   ├── grab-grisp-app.json                 # Standalone GRAB v1.1.0 Grisp multi-worker blueprint
│   └── companions/                         # Pinned Postgres 18, MariaDB 11.8, Redis, analytics, storage, automation, webmail
├── internal/
│   ├── daemon/
│   │   ├── server.go                       # Strictly 127.0.0.1 / Unix-socket authenticated control API
│   │   └── inventory.go                    # Multi-server node & port registry (127.0.0.1:8765-8772 allocation)
│   ├── container/
│   │   ├── harden.go                       # Fail-closed --read-only, cap_drop, non-root, no-secrets policy checker
│   │   ├── compose.go                      # Deterministic Compose YAML generator
│   │   └── edge.go                         # Caddy (default) / Nginx+Certbot / Cloudflare Tunnel config renderer
│   ├── catalog/
│   │   └── loader.go                       # Catalog manifest loader, digest verifier & variable validator
│   ├── rollout/
│   │   ├── grisp.go                        # Zero-downtime Grisp SIGHUP/--reload & /_ready worker drain coordinator
│   │   └── pipeline.go                     # 6-stage rollout state machine + explicit pre-rollout grab migrate hook
│   └── snapshot/
│       ├── store.go                        # Immutable SHA-256 pre-deploy snapshot archiver
│       └── rollback.go                     # Atomic restore & post-rollback /_ready verification
├── docs/
│   ├── ROADMAP.md
│   └── badges/
│       ├── built-with-grab.svg
│       ├── ecosystem-step-5.svg
│       └── spec-in-review.svg
├── specifications/
│   └── GRABTACULOUS-SPEC-001.md
└── tests/
    ├── harden_test.go                      # Container policy rejection (:latest, read_only:false, 0.0.0.0 DB, plaintext secrets)
    ├── catalog_test.go                     # Curated marketplace blueprint validation & Compose rendering tests
    ├── rollout_test.go                     # Mocked/local Grisp /_ready gating, 30s drain, SIGHUP reload & failback tests
    └── snapshot_rollback_test.go           # Pre-deploy snapshot capture, SHA-256 tamper rejection & atomic rollback tests
```

---

## 3. Container Hardening & Edge TLS Policy (`GRABTAC-E401`)

Every generated Compose stack and edge route passes strict static admission checks before execution:
1. **Mandatory App Service Hardening:** `read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, `user: "65532:65532"` (or explicit non-root service UID for curated DB images), `tmpfs` with `noexec,nosuid`, and image pinned by explicit version + digest.
2. **Forbidden Anti-Patterns (Fail-Closed `GRABTAC-E401`):**
   - `privileged: true` or `network_mode: host`
   - Mounting `/var/run/docker.sock` inside any marketplace or web app container
   - Publishing database/Redis/Grisp ports on `0.0.0.0` (must bind `127.0.0.1` or internal private Docker network)
   - Embedding secret literals (`GRABLAB_MASTER_KEY=...` literal in YAML) instead of `${VAR:?...}` env placeholders
3. **Edge Proxy TLS Options (`ADR-005`):**
   - **`caddy` (Recommended Default):** Automatic Let's Encrypt / ZeroSSL HTTPS + clean `reverse_proxy 127.0.0.1:<port>` health check against `/_ready`.
   - **`nginx`:** Nginx + Certbot ACME site block with `X-Forwarded-Proto`/`X-Forwarded-For` and `proxy_next_upstream` readiness checks.
   - **`cloudflare-tunnel`:** Zero inbound port `80`/`443` `cloudflared` ingress rule targeting `http://127.0.0.1:<port>`.

---

## 4. Loopback / SSH Control API & CLI Surface

Because **Grabtaculous** never exposes a public web panel port, `grabtaculousd` serves structured JSON commands strictly on `127.0.0.1:9440` (or Unix socket) invoked by **Gashboard** over SSH:

| Operation / CLI Subcommand | Control Plane Endpoint | Purpose |
|---|---|---|
| `grabtaculous daemon` | `GET /_health`, `GET /_ready`, `GET /v1/nodes` | Starts loopback/socket supervisor reporting active apps, `Grisp` workers, and host metrics (`ADR-025c` string numbers). |
| `grabtaculous catalog list\|inspect` | `GET /v1/catalog` | Lists pinned 1-click blueprints (`grablab-grex-bundle`, `grablab-full-stack`, `grab-grisp-app`, companions) with SHA-256 digests and required capabilities. |
| `grabtaculous deploy <blueprint>` | `POST /v1/deploy` | Validates `grabman.lock`, renders `--read-only` Compose + Edge SSL config, captures Pre-Deploy Snapshot, deploys containers, and polls `/_ready`. |
| `grabtaculous rollout <app>` | `POST /v1/rollout` | Performs zero-downtime `Grisp` binary/app/Site DNA rollout (`grab check` -> snapshot -> optional `grab migrate` -> `/_ready`-gated rolling swap -> auto-rollback on error). |
| `grabtaculous rollback <deploy_id>` | `POST /v1/rollback` | Restores an immutable Pre-Deploy Snapshot (`grabman.lock`, Compose/Edge manifests, static assets, `Site DNA`) and verifies `/_ready === 200`. |
| `grabtaculous snapshot list\|verify` | `GET /v1/snapshots` | Lists snapshots and verifies SHA-256 manifest integrity. |

---

## 5. `v0.1.0` Acceptance & Verification Gates

`Grabtaculous v0.1.0` is eligible for completion and synchronized `origin` + `stable` promotion **only** when **100%** of these gates pass:
1. **Go Build, Unit & Race Verification:**
   - `go build ./...` compiles cleanly with `CGO_ENABLED=0` across `linux/amd64`, `linux/arm64`, and `windows/amd64`.
   - `go test -count=1 ./...` passes `100%` (plus `go test -race -count=1 ./...` on Linux/CGO).
   - `govulncheck ./...` and `go mod verify` report zero issues.
2. **Security Hardening & Catalog Verification (`harden_test.go`, `catalog_test.go`):**
   - Rejects any template with `read_only: false`, missing `cap_drop: ["ALL"]`, `:latest` tags, `0.0.0.0` DB exposure, Docker socket mounts, or hardcoded secrets (`GRABTAC-E401`).
   - Verifies all generated `grablab-grex-bundle`, `grablab-full-stack`, and `grab-grisp-app` manifests match `GRAB v1.1.0` + `GrabLab CMS v1.0.1` + `Grex Theme Builder v1.0.0` + `Groxy AI v1.0.0` `--read-only` contracts.
3. **Zero-Downtime Rollout & Automatic Rollback Verification (`rollout_test.go`, `snapshot_rollback_test.go`):**
   - Spins up local test HTTP/Grisp workers simulating a rolling upgrade: proves zero failed requests while workers drain behind `/_ready === 503 -> 200`, retains last-known-good when candidate `grab check` or `/_ready` fails, and proves full automated + manual Pre-Deploy Snapshot Rollback restores byte-for-byte state and passes `/_ready`.
4. **Plain-English Release Guide & Dual-Repo Promotion (upon `v0.1.0` completion):**
   - Generates `docs/releases/pdf/Grabtaculous-v0.1.0-Guide.pdf`, `CGO_ENABLED=0` binaries + `SHA256SUMS.txt`, runs `sync-notebook-sources.ps1`, and publishes synchronized `v0.1.0` GitHub Releases with the mandatory **Release Type & Affected Layers** + **6-Layer Ecosystem Compatibility Matrix** block to `urbanspress/grabtaculous.progress` and `urbanspress/grabtaculous.stable`.
