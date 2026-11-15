# GRABTACULOUS-SPEC-100: Grabtaculous `v1.0.0` Production Contract Freeze & Cross-Layer Security Audit

- **Status:** Approved & Complete (`v1.0.0` Step 5 Production Stability Freeze Implemented, Verified & Synchronized)
- **Target Release:** `v1.0.0` (`MAJOR.MINOR.PATCH` — Step 5 Supervisor Daemon, `--read-only` Container Orchestrator & Rollout Production Freeze)
- **Foundational Baselines:**
  - **GRAB:** `v1.1.0` (`urbanspress/grab` — `Grisp` supervisor, `/_health`, `/_ready`, `grab check`, `grab migrate`, `ADR-005`, `ADR-017`, `ADR-024f`, `ADR-025g`, `ADR-027b`)
  - **Grex JS:** `v1.0.0` (`urbanspress/grex-js`)
  - **GrabLab CMS:** `v1.0.1` (`urbanspress/grablab-cms`)
  - **Grex Theme Builder:** `v1.0.0` (`urbanspress/grex-builder`)
  - **Groxy AI:** `v1.0.0` (`urbanspress/groxy-ai`)
  - **`grabman` & Gashboard:** `v1.0.0` Step 5 Freeze Companions (`specifications/GRABMAN-SPEC-100.md`, `specifications/GASHBOARD-SPEC-100.md`)
- **Governance:** Read alongside [`GRABTACULOUS-EXPERT.md`](../GRABTACULOUS-EXPERT.md), [`AGENTS.md`](../AGENTS.md), [`GRABTACULOUS-SPEC-001.md`](GRABTACULOUS-SPEC-001.md), and [`docs/ROADMAP.md`](../docs/ROADMAP.md).

---

## 1. Executive Summary (Plain English)

Before promoting Step 5 (`grabman`, **Grabtaculous**, and **Gashboard**) to `v1.0.0` for **Step 6 (Groxy Desk)**, an independent pre-`v1.0.0` cross-layer audit (`Orchestrator (Argon)`) verified every security boundary inside **Grabtaculous**: its stealth loopback/Unix-socket control daemon (`grabtaculousd`), its `--read-only` container admission checker, its curated SHA-256 app catalog, its `/_ready`-gated zero-downtime `Grisp` rolling swap pipeline, and its atomic Pre-Deploy Snapshot rollback engine.

This specification documents the verified audit evidence and freezes the **`grabtaculous v1.0.0`** CLI and loopback `/v1/*` API contracts with 100% backwards compatibility for all `v0.1.0` deployments and **Gashboard** workflows.

---

## 2. Independent Pre-`v1.0.0` Security & Cross-Layer Audit Evidence

| Audit Domain | Audited Implementation Files | Verified Finding |
|---|---|---|
| **Loopback-Only Stealth Control Plane & Constant-Time Bearer Auth (`ADR-005`)** | [`internal/daemon/server.go`](../internal/daemon/server.go), [`cmd/grabtaculous/main.go`](../cmd/grabtaculous/main.go) | `ValidateListenAddress` strictly permits `127.0.0.1:<port>`, `localhost:<port>`, `[::1]:<port>`, or `unix:<path>` and fails closed (`GRABTAC-E501`) on `0.0.0.0`, `::`, `*`, or non-loopback IPs. Auto-generates a 32-byte random `control.token` (`0600` atomic write) and validates `Authorization: Bearer <token>` on every `/v1/*` endpoint using `subtle.ConstantTimeCompare`. |
| **`GRAB-E401` Path Confinement & Atomic Writes** | [`internal/safepath/pathguard.go`](../internal/safepath/pathguard.go), [`internal/safepath/errors.go`](../internal/safepath/errors.go) | Blocks `\x00` NUL bytes, UNC `\\`/`//` shares, `..` traversal, escaping symlinks (`EvalExistingSymlinks`), `.git`, `.env`, `.env.*` (except explicit `.env.example`), `.ssh`, `id_rsa`, `id_ed25519`, `id_ecdsa`, `id_dsa`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, and reads > `10 MiB` (`MaxReadBytes`). Uses temp-file + `Chmod` + atomic `Rename`. Emits `GRABTAC-E201` (`GRAB-E401` parity). |
| **`--read-only` Container Hardening Admission (`GRABTAC-E401`)** | [`internal/container/harden.go`](../internal/container/harden.go), [`internal/container/compose.go`](../internal/container/compose.go), [`internal/container/edge.go`](../internal/container/edge.go) | Rejects missing `read_only: true`, missing `cap_drop: ["ALL"]`, missing `security_opt: ["no-new-privileges:true"]`, `privileged: true`, `network_mode: host`, `/var/run/docker.sock` mounts, `:latest` tags, non-loopback `0.0.0.0`/bare ports, and hardcoded secrets. Renders hardened Compose stacks plus Caddy, Nginx + Certbot, or Cloudflare Tunnel edge blocks routing to `127.0.0.1:8765–8772`. |
| **Curated SHA-256 App Catalog** | [`internal/catalog/loader.go`](../internal/catalog/loader.go), [`catalog/index.json`](../catalog/index.json) | Loads embedded & optional external curated JSON blueprints (`grablab-grex-bundle`, `grablab-full-stack`, `grab-grisp-app`, `postgres-18`, `mariadb-11-8`, `redis-standalone`, `analytics`, `automation`, `object-storage`, `webmail`) and fails closed (`GRABTAC-E301`) on any SHA-256 digest mismatch. |
| **Zero Shell-String Interpolation & `/_ready`-Gated Rollouts (`ADR-024f`)** | [`internal/rollout/pipeline.go`](../internal/rollout/pipeline.go), [`internal/rollout/grisp.go`](../internal/rollout/grisp.go) | Executes `grabman solve --verify`, `grab check`, and explicit pre-rollout `grab migrate` via structured `exec.CommandContext` argv slices only. Supports 6-stage `/_ready`-gated sequential `Grisp` worker draining (`30s` drain budget, `127.0.0.1:8765–8772` port allocation) and triggers automatic rollback on any `/_ready` or smoke failure (`GRABTAC-E601`). |
| **Immutable Pre-Deploy Snapshots & Atomic Rollback** | [`internal/snapshot/store.go`](../internal/snapshot/store.go), [`internal/snapshot/rollback.go`](../internal/snapshot/rollback.go) | Captures SHA-256-indexed snapshots before every deploy/rollout and fails closed (`GRABTAC-E701`) on file tampering, restoring prior manifests and verifying `/_ready === 200`. |

### Baseline Audit Verification Results Observed
- `go build ./...` (from `C:\Development\grabtaculous`): **PASS** (`0` errors).
- `go test -count=1 ./...`: **PASS** (`ok grabtaculous/tests 10.028s`, `0` skipped tests).
- `govulncheck ./...` (`go1.27.4`): **PASS** (`No vulnerabilities found.`).

---

## 3. Frozen `v1.0.0` Contract Specifications (For Gashboard & Step 6 Groxy Desk)

Upon project-owner approval of this specification, the `v1.0.0` release implementation freezes the following contracts across all `v1.x` releases:

### 3.1 Loopback Control Daemon Endpoints (`127.0.0.1:9440` or `unix:<path>`)

| Method & Path | Auth | Frozen `v1.x` Contract |
|---|---|---|
| `GET /_health` | Loopback local | Returns `200 OK` JSON liveness metadata (`ok`, `status: "ok"`, `service: "grabtaculousd"`, `version`). |
| `GET /_ready` | Loopback local | Returns `200 OK` JSON readiness metadata (`ok`, `ready: true`). |
| `GET /v1/nodes` | `Bearer <control.token>` (`ConstantTimeCompare`) | Lists registered nodes, allocated `Grisp` worker ports (`127.0.0.1:8765–8772`), and status. |
| `POST /v1/nodes` | `Bearer <control.token>` | Registers/updates managed node metadata (strictly loopback-controlled). |
| `GET /v1/catalog` | `Bearer <control.token>` | Returns curated, SHA-256-verified blueprints and required capabilities. |
| `POST /v1/deploy` | `Bearer <control.token>` | Validates `grabman.lock`, renders `--read-only` Compose + Edge SSL (`caddy` \| `nginx` \| `cloudflare-tunnel`), captures Pre-Deploy Snapshot, deploys containers, returns `deploy_snapshot_id` & `pre_deploy_snapshot`. |
| `POST /v1/rollout` | `Bearer <control.token>` | Executes 6-stage zero-downtime rollout (`grabman solve --verify` -> `grab check` -> snapshot -> optional `grab migrate` -> `/_ready` swap -> auto-failback on error). |
| `POST /v1/rollback` | `Bearer <control.token>` | Verifies Pre-Deploy Snapshot SHA-256 digest, atomically restores files/configs, and verifies `/_ready === 200`. |
| `GET /v1/snapshots` | `Bearer <control.token>` | Lists recorded Pre-Deploy Snapshots with integrity metadata. |
| `GET /v1/status` | `Bearer <control.token>` | Reports deployed apps, snapshots, and daemon runtime version. |

### 3.2 CLI Command Surface & Error Registry (`cmd/grabtaculous/main.go`, `internal/safepath/errors.go`)

- **CLI Subcommands:** `daemon`, `catalog <list|inspect>`, `deploy <blueprint-id>`, `rollout [<dir>]`, `rollback <deploy-id>`, `snapshot <list|create|verify>`, `audit`, `status`, `version` (with `--json` structured output across all subcommands).
- **Version Promotion:** Promote `Version = "1.0.0"` (`cmd/grabtaculous/main.go`) and `DaemonVersion = "1.0.0"` (`internal/daemon/server.go`), emitting `"version": "1.0.0"` and `"schema_version": "1.0.0"` while maintaining backwards-compatible handling of `"0.1.0"` workspace/catalog/snapshot metadata.
- **Frozen Error Code Registry (`GRABTAC-E101..E701`):**
  - `GRABTAC-E101`: Preflight failure (`grabman solve --verify` or `grab check` failed)
  - `GRABTAC-E201`: `GRAB-E401` path confinement or sensitive-path rejection
  - `GRABTAC-E301`: Curated catalog manifest or SHA-256 verification failure
  - `GRABTAC-E401`: Container `--read-only` / security policy admission failure
  - `GRABTAC-E501`: Control-plane non-loopback bind rejection (`ADR-005`) or unauthorized Bearer token
  - `GRABTAC-E601`: Rollout `/_ready` or health drain failure
  - `GRABTAC-E701`: Snapshot creation, tamper check, or rollback restore failure

---

## 4. `v1.0.0` Approval & Release Acceptance Gates (Completed)

Approved by the project owner and verified for Step 5 `v1.0.0` production release:
1. [x] Update `Version` / `DaemonVersion` to `"1.0.0"` and `"schema_version"` handling to support both `"0.1.0"` and `"1.0.0"` cleanly in `C:\Development\grabtaculous`.
2. [x] Update CLI/daemon tests (`tests/daemon_and_cli_test.go`, etc.) and verify `go build ./...`, `go vet ./...`, `go test -count=1 ./...` (`100%` pass), `govulncheck ./...` (`0` vulnerabilities), `go mod verify`, and live E2E integration with `grabman v1.0.0` and `gashboard v1.0.0`.
3. [x] Build static `CGO_ENABLED=0` binaries (`windows/amd64`, `linux/amd64`, `linux/arm64`), generate `docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf` and `SHA256SUMS.txt`, run `sync-notebook-sources.ps1`, and publish synchronized `v1.0.0` releases on `urbanspress/grabtaculous.progress` and `urbanspress/grabtaculous.stable`.
