# Grabtaculous

![Built with GRAB v1.1.0](docs/badges/built-with-grab.svg)
![GRAB Ecosystem Step 5 — Grabtaculous](docs/badges/ecosystem-step-5.svg)
![Release v1.0.0 — Complete](docs/badges/release-v1.0.0.svg)

**Grabtaculous** is the Multi-Server Supervisor Daemon, `--read-only` Container & 1-Click App Marketplace Orchestrator, Zero-Downtime `Grisp` Rollout Engine, and Pre-Deploy Snapshot Rollback System for **Step 5 of the Post-1.0 GRAB Ecosystem**.

- **Current Status:** `v1.0.0` — **Complete (Production Freeze)** (Plain-English Walkthrough: [`Grabtaculous-v1.0.0-Guide.pdf`](docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf) · Spec: [`GRABTACULOUS-SPEC-100.md`](specifications/GRABTACULOUS-SPEC-100.md)).
- **Development Repo (`origin`):** `https://github.com/urbanspress/grabtaculous.progress`
- **Production Release Repo (`stable`):** `https://github.com/urbanspress/grabtaculous.stable`

---

## Core Architecture Pillars (`v1.0.0`)

1. **Zero Public Admin Surface (`ADR-005`, `ADR-017`):**
   - `grabtaculousd` runs exclusively on loopback (`127.0.0.1`) or Unix sockets on managed VPS hosts and communicates with **Gashboard** strictly over encrypted SSH—eliminating public VPS panel login pages entirely.
2. **Hardened `--read-only` Container & Edge TLS Orchestration:**
   - Every container stack enforces `read_only: true`, `cap_drop: ["ALL"]`, `no-new-privileges:true`, non-root UID `65532`, loopback port binding, private database networking, and automatic HTTPS at the edge via **Caddy** (default), **Nginx + Certbot**, or **Cloudflare Tunnel**.
3. **Curated 1-Click App Marketplace (`catalog/`):**
   - Pinned, digest-verified blueprints for **`GrabLab CMS v1.0.1 + Grex Theme Builder v1.0.0 + Groxy AI v1.0.0`**, standalone `Grisp` apps, private Postgres/MariaDB/Redis databases, and companion self-hosted apps—zero unverified scripts or `:latest` tags.
4. **Zero-Downtime `Grisp` Rollouts & Pre-Deploy Snapshot Rollbacks:**
   - Enforces `grabman solve --verify` + `grab check`, captures an immutable Pre-Deploy Snapshot before every rollout, swaps workers/generations safely behind `/_ready` probes (`30s` drain), and rolls back automatically or in 1 click if anything fails.

---

## 6-Layer Ecosystem Compatibility Matrix

| Ecosystem Layer | Repository | Required Version Range | Role |
|---|---|---|---|
| **Core Language, Bytecode VM & Grisp Server** | `urbanspress/grab` | `>= v1.1.0 (< v2.0.0)` | Executes `Grisp` workers, `/_health`, `/_ready`, `server.asset`, `grab migrate` |
| **Reactive Frontend Runtime & 20-Element Library** | `urbanspress/grex-js` | `>= v1.0.0 (< v2.0.0)` | Zero-`<div>` UI contracts & compiled static bundles |
| **Flagship Zero-Plugin CMS** | `urbanspress/grablab-cms` | `>= v1.0.1 (< v2.0.0)` | 1-click CMS stack with `--read-only` Compose contract |
| **Exclusive `mode: "visual"` Theme & Studio** | `urbanspress/grex-builder` | `>= v1.0.0 (< v2.0.0)` | Auto-wired exclusive visual theme & `/static/grex/` assets |
| **Autonomous Design, Content & SEO Agent** | `urbanspress/groxy-ai` | `>= v1.0.0 (< v2.0.0)` | Optional AI stack & `Site DNA` snapshot payloads |
| **Scaffolder, Orchestrator & Discreet VPS Panel** | `urbanspress/grabman`, `urbanspress/grabtaculous`, `urbanspress/gashboard` | `>= v0.1.0 (< v2.0.0)` (`v1.0.0` Frozen) | Lockfile verification, `--read-only` orchestration, zero-downtime rollouts & rollback |

---

## Quick Start (`v1.0.0`)

Build or install the static zero-dependency binary (`CGO_ENABLED=0`), deploy a hardened `--read-only` stack, audit Compose files, and manage zero-downtime rollouts and atomic snapshot rollbacks:

```bash
# Build static binary cleanly from source (pure Go stdlib, zero external dependencies)
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=1.0.0" -o grabtaculous ./cmd/grabtaculous

# Browse curated 1-click blueprints
grabtaculous catalog list
grabtaculous catalog inspect grablab-grex-bundle

# Deploy a locked-down --read-only stack + Caddy automatic HTTPS
grabtaculous deploy grablab-grex-bundle --dir ./my-site --domain shop.example.com --edge-proxy caddy

# Audit Compose security + run a 6-stage zero-downtime Grisp rollout
grabtaculous audit --dir ./my-site --require-digest
grabtaculous rollout --dir ./my-site --entry src/app.grab --migrate-file migrations/001_init.grab

# Inspect snapshots or roll back cleanly in one command
grabtaculous snapshot list --dir ./my-site
grabtaculous rollback <deploy-id> --dir ./my-site

# Launch stealth loopback supervisor daemon (never binds public 0.0.0.0)
grabtaculous daemon --listen 127.0.0.1:9440 --state-dir ./.grabtaculous
```

## Governance & Specifications

- [Agent Rules (`AGENTS.md`)](AGENTS.md)
- [Engineering Invariants (`GRABTACULOUS-EXPERT.md`)](GRABTACULOUS-EXPERT.md)
- [Plain-English Release Guide (`Grabtaculous-v1.0.0-Guide.pdf`)](docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf)
- [`v1.0.0` Production Freeze Specification (`specifications/GRABTACULOUS-SPEC-100.md`)](specifications/GRABTACULOUS-SPEC-100.md)
- [`v0.1.0` Specification (`specifications/GRABTACULOUS-SPEC-001.md`)](specifications/GRABTACULOUS-SPEC-001.md)
- [Roadmap (`docs/ROADMAP.md`)](docs/ROADMAP.md)
- [Changelog (`CHANGELOG.md`)](CHANGELOG.md)
- [Handoff Log (`HANDOFF.md`)](HANDOFF.md)
- [License (`LICENSE` — MIT)](LICENSE)
