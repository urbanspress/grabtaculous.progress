# GRABTACULOUS-EXPERT.md — Non-Negotiable Engineering Invariants

This checklist defines the **never-break invariants** for **Grabtaculous** (`urbanspress/grabtaculous`), the Multi-Server Supervisor Daemon, `--read-only` Container & Marketplace Orchestrator, Zero-Downtime `Grisp` Rollout Engine, and Pre-Deploy Snapshot Rollback System in **Step 5 of the Post-1.0 GRAB Ecosystem**. Every specification, catalog template, rollout controller, and PR must satisfy all four pillars.

---

## 1. Zero Public Control-Plane Surface & Private Loopback Workers (`ADR-005`, `ADR-017`)
- **No Public Admin Panel Ports:** `grabtaculousd` MUST listen exclusively on private loopback (`127.0.0.1`) or rootless Unix sockets—never `0.0.0.0` unauthenticated management HTTP listeners. Remote access from **Gashboard** travels exclusively over authenticated SSH tunnels.
- **Unprivileged App Workers (`127.0.0.1:8765–8772`):** `Grisp` processes and application containers MUST never run as `root` and MUST never manage TLS certificates directly. Automatic HTTPS (Let's Encrypt / ZeroSSL) stays cleanly at the edge reverse proxy (**Caddy** recommended, **Nginx + Certbot**, or **Cloudflare Tunnel**) using `--trusted-proxy 127.0.0.1`.
- **Private Data Stores:** Provisioned PostgreSQL, MariaDB, and Redis containers MUST bind strictly to internal bridge networks or `127.0.0.1` loopback—never `0.0.0.0:5432`, `0.0.0.0:3306`, or `0.0.0.0:6379`. Remote access uses **Gashboard**'s encrypted SSH tunnel (`127.0.0.1`).

---

## 2. Hardened `--read-only` Container & Curated Marketplace Isolation
- **Mandatory Container Hardening:** Every orchestrated GRAB/Ecosystem service manifest MUST enforce:
  - `read_only: true`
  - `cap_drop: ["ALL"]`
  - `security_opt: ["no-new-privileges:true"]`
  - Non-root UID (`65532:65532`)
  - Bounded `tmpfs: ["/tmp:rw,noexec,nosuid,size=64m"]`
  - Strictly scoped writable mounts (`storage/`, `uploads/`, `public/grex/`) while source, themes, and manifests stay `:ro`.
- **Pinned, Curated Marketplace Only:** Marketplace blueprints (`catalog/`) MUST use exact version tags/SHA-256 digests (`:latest` forbidden), zero `curl | sh` remote scripts, zero Docker-socket mounts inside app containers, and zero hardcoded secrets (`GRABLAB_MASTER_KEY`, DB passwords supplied strictly via runtime env/secret descriptors).

---

## 3. Zero-Downtime `Grisp` Rollouts & Explicit Migration Separation (`ADR-024f`, `ADR-025g`)
- **Lockfile & Static Gate Before Touching Traffic:** Every rollout MUST pass `grabman solve --verify` and `grab check` before draining or replacing live workers.
- **`/_ready`-Gated Rolling Replacement:** Rolling upgrades across `Grisp` workers (`--workers N --worker-base-port P`, `SIGHUP` / `--reload`) MUST verify `GET /_health` + `GET /_ready === 200` on new generations before deregistering old workers (`30s` drain window) and retain last-known-good apps automatically on failure.
- **Explicit Pre-Rollout Migrations Only (`ADR-024f`):** Database migrations (`grab migrate`) MUST run explicitly prior to worker traffic switchover and NEVER silently inside live HTTP reloads.

---

## 4. Mandatory Pre-Deploy Snapshots, Atomic Rollback & Dual-Repo Discipline
- **Snapshot Before Every Mutation:** Never deploy, upgrade, or apply `Site DNA` without first recording an immutable Pre-Deploy Snapshot (`grabman.lock`, Compose/Caddy config, `public/grex/` static hashes, `Site DNA`, and pre-deploy DB backup hook metadata).
- **1-Click & Auto-Failback Rollback:** Any `/_ready` gate failure or post-deploy verification timeout MUST immediately trigger automatic restoration of the Pre-Deploy Snapshot without corrupting running sites.
- **Dual-Repo Protection & 6-Layer Matrix:** Keep emerald `#10B981` badges committed in `docs/badges/`, never push unverified code to `urbanspress/grabtaculous.stable`, and accompany every release with the **Release Type & Affected Layers** callout, **6-Layer Ecosystem Compatibility Matrix**, and plain-English walkthrough PDF.
