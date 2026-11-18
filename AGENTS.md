# AGENTS.md — Grabtaculous

**Grabtaculous** (`urbanspress/grabtaculous.progress` / `urbanspress/grabtaculous.stable`, local directory `C:\Development\grabtaculous`) is the Multi-Server Supervisor Daemon, `--read-only` Container & Marketplace Orchestrator, Zero-Downtime `Grisp` Rollout Engine, and Pre-Deploy Snapshot Rollback Daemon for **Step 5 of the Post-1.0 GRAB Ecosystem**.

---

## 1. Core Invariants & Upstream Scope
1. **Read [`GRABTACULOUS-EXPERT.md`](GRABTACULOUS-EXPERT.md) and [`VPS-DEPLOY-TEST-FIRST.md`](VPS-DEPLOY-TEST-FIRST.md) first** before drafting any specification, editing any file, deploying to any VPS, or reviewing any pull request.
2. **Respect Ecosystem Layer Boundaries:** Treat **GRAB `v1.1.0`** (`Grisp`, `grab serve --workers --reload`, `/_health`, `/_ready`, `server.asset`, `grab migrate`, `ADR-005`, `ADR-017`), **Grex JS `v1.0.0`**, **GrabLab CMS `v1.0.1`**, **Grex Theme Builder `v1.0.0`**, **Groxy AI `v1.0.0`** (`deploy/docker-compose.yml`, `Site DNA`), and **`grabman v0.1.0`** (`grabman.lock`, `grabman doctor`) as distinct upstream/peer contracts. Never modify core GRAB Go compiler internals or `grablab-cms` application source inside `grabtaculous`.
3. **Zero Public Control-Panel Port Exposure (`ADR-005`):** `grabtaculousd` binds strictly to private loopback (`127.0.0.1`) or rootless Unix sockets—never exposing a public unauthenticated management port on `0.0.0.0`. Remote **Gashboard** connections arrive exclusively over authenticated SSH tunnels.
4. **Hardened `--read-only` Containers & Explicit Rollbacks:** Every app/marketplace container template MUST enforce `read_only: true`, `cap_drop: ["ALL"]`, `no-new-privileges:true`, non-root UID (`65532:65532`), loopback port publishing (`127.0.0.1`), and pinned image digests—backed by mandatory Pre-Deploy Snapshots before any rollout.

---

## 2. Workflow, Spec Gate & Verification Gates
- **Spec-First Approval Gate:** Do NOT write `grabtaculous` Go application code (`cmd/`, `internal/`, `catalog/`, `tests/`) until the active milestone specification (starting with `specifications/GRABTACULOUS-SPEC-001.md`) has been reviewed and explicitly approved by the project owner.
- **Plain-English User Communication:** The project owner directs development in plain English. Always explain container orchestration, rollouts, SSL proxy choices, and rollback steps simply without unnecessary jargon. Handle all file edits, git commands, and terminal verification directly.
- **No Unapproved Commits/Pushes:** Do not commit, push, or publish releases without explicit project-owner approval (explicitly requested repository bootstraps excepted).
- **Mandatory Test & Lint Gate:** Before marking any implementation milestone complete or requesting release promotion:
  1. Run `go build ./...` and `go test -count=1 ./...` on Windows (plus `go test -race -count=1 ./...` on Linux / CGO-enabled runners) with **100% passing results**.
  2. Run `govulncheck ./...` and `go mod verify` cleanly.
  3. Verify generated `--read-only` Docker Compose + Caddy/Nginx edge manifests, `/_ready`-gated zero-downtime `Grisp` rollout tests, and automated pre-deploy snapshot rollback tests pass `100%`.

---

## 3. Two-Repository Workflow & Release Protection
- **Repository Roles:**
  - `origin` (`https://github.com/urbanspress/grabtaculous.progress.git`): Active development repository for specifications, drafts, work-in-progress checkpoints, and milestone commits.
  - `stable` (`https://github.com/urbanspress/grabtaculous.stable.git`): Production release repository for 100% verified milestone releases only.
- **Strict `stable` Protection:** All AI agents and models MUST push work-in-progress commits, unapproved specs, and partial checkpoints ONLY to `origin` (`urbanspress/grabtaculous.progress`). Pushing to `stable` (`urbanspress/grabtaculous.stable`) is strictly forbidden unless the milestone is 100% `Complete` in `docs/ROADMAP.md`, all verification gates pass 100%, and `HANDOFF.md` records no pending milestone tasks.
- **Synchronized Releases & Walkthrough PDFs:** Whenever a completed milestone (`v0.1.0` onward) is promoted to `stable`, push `main` and annotated version tags in strict ascending chronological order to both `origin` and `stable`, build `CGO_ENABLED=0` static binaries (`grabtaculous-linux-amd64`, `grabtaculous-linux-arm64`, `grabtaculous-windows-amd64.exe`), generate `SHA256SUMS.txt` and the plain-English walkthrough PDF (`docs/releases/pdf/Grabtaculous-v<X.Y.Z>-Guide.pdf`), and publish matching GitHub Releases + assets on **both** repositories (`urbanspress/grabtaculous.progress` and `urbanspress/grabtaculous.stable`).

### SemVer (MAJOR.MINOR.PATCH) & 6-Layer Ecosystem Compatibility Matrix Standard
1. **Strict `MAJOR.MINOR.PATCH` Semantic Versioning (`ADR-027c`):**
   - **`0.x.y` (`PRE-1.0`):** Pre-`1.0.0` Step 5 orchestration & marketplace deliverables establishing RPC/container/snapshot compatibility ahead of `v1.0.0` freeze.
   - **`1.0.x` (`PATCH`):** Strictly for non-breaking bug fixes, security hardening, documentation polish, and cross-layer audit patches (`100%` drop-in compatible).
   - **`1.x.0` (`MINOR`):** For backwards-compatible new features, additive catalog templates, or telemetry/rollout hooks.
   - **`2.0.0` (`MAJOR`):** Reserved for breaking architectural or contract changes—**never used without explicit project-owner approval (`ADR-027c`)**.
2. **Mandatory Release Compatibility Badge & 6-Layer Matrix Block:**
   - Every GitHub Release across both `.progress` and `.stable` repositories MUST display at the top of its release notes:
     1. A **Release Type & Affected Layers** summary callout (`PRE-1.0 (v0.x.y)` | `PATCH (v1.0.x)` | `MINOR (v1.x.0)`, which layer(s) changed, and whether any neighboring layer requires an upgrade).
     2. A **6-Layer Ecosystem Compatibility Matrix** Markdown table covering all six ecosystem layers:

````markdown
### Release Type & Affected Layers
- **Release Version & SemVer Tier:** `v<X.Y.Z>` (`PRE-1.0 v0.x.y` | `PATCH 1.0.x — Non-Breaking Bug/Security/Audit Patch` | `MINOR 1.x.0 — Backwards-Compatible Feature Release`)
- **Affected Layer(s):** `<Layer Name>` (`urbanspress/<repo>`)
- **Neighboring Layer Upgrade Required?** `No — 100% drop-in compatible` (or explicit version requirement if `Yes`)

| Ecosystem Layer | Repository | Required Version Range | Touched in This Release? | Neighbor Upgrade Required? |
|---|---|---|---|---|
| **Core Language, Bytecode VM & Grisp Server** | `urbanspress/grab` | `>= v1.1.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Reactive Frontend Runtime & 20-Element Library** | `urbanspress/grex-js` | `>= v1.0.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Flagship Zero-Plugin CMS** | `urbanspress/grablab-cms` | `>= v1.0.1 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Exclusive `mode: "visual"` Theme & Studio** | `urbanspress/grex-builder` | `>= v1.0.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Autonomous Design, Content & SEO Agent** | `urbanspress/groxy-ai` | `>= v1.0.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Scaffolder, Orchestrator & Discreet VPS Panel** | `urbanspress/grabman`, `urbanspress/grabtaculous`, `urbanspress/gashboard` | `>= v0.1.0` | `Yes` / `No` | `No` |
````

### Mandatory Milestone Snapshot Rule
- **Ecosystem Snapshot Sync:** At the end of every milestone (before closing the milestone or pushing release promotions), every agent MUST run:
  ```powershell
  powershell -ExecutionPolicy Bypass -File C:\Development\GRAB\scripts\sync-notebook-sources.ps1
  ```
  so `C:\Development\ECOSYSTEM-SNAPSHOT.md` always reflects the latest Git branches, commits, tags, `AGENTS.md`, `*-EXPERT.md`, `HANDOFF.md`, `docs\ROADMAP.md`, and `specifications\*.md` across all discovered `C:\Development\*` repositories.

---

## 4. 4-Tier Model Routing & Tandem Handoff Protocol

### 4-Tier Antigravity Model Routing
Before ending a turn or starting a new phase, advise the project owner which Antigravity model and effort level to select for the next step so AI Pro quota is used efficiently:
1. **`Google Hydrogen (Medium)`:** Pre-implementation specifications (`*-SPEC-*.md`), documentation, governance updates, and single-package edits.
2. **`Google Hydrogen (High)`:** Standard feature milestone implementations (`v0.1.0` daemon/catalog slices, unit test suites).
3. **`Orchestrator (Argon)`:** Second-opinion pre-`v1.0.0` security/container/rollout audits, multi-repo cross-layer integrations (`grabman` + `grabtaculous` + `gashboard` + `GRAB`), and complex refactors where Argon-level planning + automated delegation saves tokens.
4. **`Google Hydrogen (Extra)`:** Reserved for multi-server orchestration (`Gashboard + Grabtaculous + grabman`), zero-downtime rollout & rollback state machines, and stubborn cross-layer bugs.
- **Mandatory Pause & Swap Prompt Rule:** Every active AI agent MUST pause at milestone boundaries or before high-risk cross-layer/pre-`v1.0.0` audits and explicitly tell the user when to switch to `Orchestrator (Argon)`, including a clear 1-sentence reason why `Orchestrator (Argon)` is recommended next.

### Tier 1 vs. Tier 2 Strict Single-Package Routing
- **Tier 1 — Frontier Models (`Google Hydrogen`, `Gemini 4 Argon`, `GPT-6.1 Sol`, `Claude Opus/Sonnet`):** Authorized to draft specifications, implement multi-package supervisor/orchestrator/rollback engines, and execute cross-layer verification suites.
- **Tier 2 — Fast Workhorse & Backup Models (`Gemini 3.8 Flash`, `OpenCode`, `Cline`, `Kilo Code`):** Must operate in **Strict Single-Package Mode** for all Go changes:
  1. **Read `GRABTACULOUS-EXPERT.md` first** before making any edit.
  2. **Scope edits to one Go package at a time** instead of multi-package mega-edits.
  3. **Verify immediately after every edit** (`go test -count=1 ./<package>` on Windows or `go test -race ./<package>` on CGO-enabled Linux) before moving to the next package.

### Tandem Handoff Protocol (`Google Antigravity <-> OpenAI Codex`)
- **Google Antigravity -> Codex:** If an issue remains unresolved after 2 attempts on `Google Hydrogen (Extra)` or `Orchestrator (Argon)`, stop editing, revert only the specific files edited during the failed attempt (asking the owner first if pre-existing uncommitted work would be affected), record what was tried in `HANDOFF.md`, update `Current Owner: OpenAI Codex`, and say: `"Ready to hand off to Codex. Type use-codex to continue."`
- **Codex -> Google Antigravity:** When Codex completes an architectural or verification task, update `HANDOFF.md`, set `Current Owner: Google Antigravity`, and say: `"Core task complete—ready to hand off to Google Antigravity. Type use-antigravity to continue."`
- **Automatic Pickup:** On pickup (`use-antigravity` or `use-codex`), immediately read `GRABTACULOUS-EXPERT.md`, `AGENTS.md`, and `HANDOFF.md`, run `go test -count=1 ./...` to confirm a clean baseline, and continue the next pending task.
