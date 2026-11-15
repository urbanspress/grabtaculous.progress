# Grabtaculous Roadmap

**Repositories:** `urbanspress/grabtaculous.progress` (active development) / `urbanspress/grabtaculous.stable` (verified releases)  
**Foundational Baselines:** **GRAB `v1.1.0`**, **Grex JS `v1.0.0`**, **GrabLab CMS `v1.0.1`**, **Grex Theme Builder `v1.0.0`**, **Groxy AI `v1.0.0`**, and **`grabman v0.1.0`**

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

**Status:** Planned

Planned Scope:
- Independent `Orchestrator (Argon)` container-escape, SSH control-channel, rollout drain, and snapshot rollback audit.
- Signed Linux (`amd64`/`arm64`) & Windows (`amd64`) static binaries, walkthrough PDF, and synchronized `v1.0.0` release on `origin` and `stable`.
