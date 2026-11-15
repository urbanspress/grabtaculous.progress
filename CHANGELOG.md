# Changelog — Grabtaculous

All notable changes to **Grabtaculous** (Step 5 Multi-Server Supervisor, `--read-only` Container Orchestrator, Curated 1-Click Marketplace & Zero-Downtime Rollout Engine) are documented in this file.

## [v1.0.0] — 2026-11-15

### Production Stability Freeze (`GRABTACULOUS-SPEC-100`)
- **SemVer Tier:** `v1.0.0 — Step 5 Production Stability Freeze` (`100%` backwards compatible with all `v0.1.0` deployments, `grabman v0.1.0..v1.0.0`, and **Gashboard v1.0.0** SSH tunnels).
- **Daemon & CLI `v1.0.0` Contract Freeze:**
  - Promoted `Version` (`cmd/grabtaculous/main.go`), `DaemonVersion` (`internal/daemon/server.go`), catalog metadata (`catalog/index.json`), deploy/inventory/rollout/snapshot schemas, and generated Compose/edge comment headers to **`1.0.0`**.
  - Aligned preflight `grabman` remediation and binary integration cleanly with **`grabman v1.0.0`**.
  - Verified zero vulnerabilities via `govulncheck ./...` (`go1.27.4`), `go mod verify`, `go vet ./...`, and `go test -count=1 ./...` (`100%` pass).
- **Cross-Platform Release Artifacts:**
  - Built static `CGO_ENABLED=0` binaries for `windows/amd64`, `linux/amd64`, and `linux/arm64`, alongside [`docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf`](docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf), `grabtaculous-v1.0.0-bundle.zip`, and `SHA256SUMS.txt`.

## [v0.1.0] — 2026-11-15

### Release Scope (`PRE-1.0` Milestone 1)
- **Stealth Loopback Supervisor Daemon (`grabtaculousd`):**
  - Strictly binds to loopback (`127.0.0.1:9440`, `[::1]:<port>`) or Unix domain sockets; fails closed with `GRABTAC-E501` if any public interface (`0.0.0.0`, `[::]`, or non-loopback IP) is requested (`ADR-005`).
  - Generates a cryptographic `0600` per-node bearer token (`control.token`), authenticates local/SSH-tunneled requests via constant-time comparison (`subtle.ConstantTimeCompare`), and persists durable atomic server/app inventory (`inventory.json`).
  - Exposes control-plane HTTP API endpoints (`GET /v1/health`, `GET /v1/nodes`, `POST /v1/nodes`, `GET /v1/catalog`, `GET /v1/apps`, `POST /v1/deploy`, `POST /v1/rollout`, `POST /v1/rollback`, `GET /v1/snapshots`, `POST /v1/snapshots/verify`) for local CLI and upcoming `Gashboard` SSH tunnels.

- **Fail-Closed `--read-only` Container & Edge Proxy Orchestrator:**
  - Enforces hardened container defaults on every generated and audited `deploy/docker-compose.yml`: `read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, non-root UID (`65532:65532`), `noexec,nosuid,nodev` `/tmp` mounts, loopback-only host bindings (`127.0.0.1`), and SHA-256 digest pinning (`GRABTAC-E301`).
  - Rejects `:latest` image tags, `privileged: true`, `network_mode: host`, `docker.sock` mounts, `0.0.0.0` host port exposure, and plaintext secrets or embedded DSN credentials (`GRABTAC-E401`).
  - Generates production edge reverse-proxy configurations with forwarded-header trust (`--trusted-proxy 127.0.0.1`) for **Caddy** (default automatic HTTPS), **Nginx + Certbot**, and **Cloudflare Tunnel**.

- **Curated SHA-256 1-Click App Marketplace (`catalog/`):**
  - Embeds 10 digest-pinned, SHA-256-indexed blueprints via Go `//go:embed` with tamper detection (`GRABTAC-E201`):
    - **Ecosystem Stacks:** `grablab-grex-bundle`, `grablab-full-stack`, `grab-grisp-app`
    - **Private Data Stores (`127.0.0.1` only):** `postgres-18`, `mariadb-11-8`, `redis-standalone`
    - **Self-Hosted Companions:** `analytics`, `object-storage`, `automation`, `webmail`

- **6-Stage Zero-Downtime `Grisp` Rollout Engine:**
  - Executes non-destructive preflight checks before touching live traffic: `grabman solve --verify`, `grabman dna validate`, `grab check --diagnostics production` (automatically wiring declared `grab.data.json` and `grab.platform.json` capability flags), and `--read-only` Compose/secret audits.
  - Captures an immutable Pre-Deploy Snapshot before mutating state, runs explicit `grab migrate --db-config ... --allow-db <alias>:migrate` hooks (`ADR-024f`), swaps upstream `Grisp` workers (`127.0.0.1:8765–8772`) strictly gated on `/_ready === 200`, verifies post-cutover health/smoke endpoints, and triggers automatic rollback on any failure.

- **Atomic Pre-Deploy Snapshot & Rollback Engine:**
  - Archives `grabman.lock`, `grabman.json`, `grab.data.json`, `grab.platform.json`, `deploy/docker-compose.yml`, `deploy/Caddyfile`, `deploy/nginx.conf`, `deploy/cloudflared.yml`, `.env.example`, `src/`, `views/`, `theme/`, `public/`, `storage/site-dna.json`, and optional DB checkpoints with a per-file SHA-256 `snapshot.json` manifest (`GRABTAC-E701`).
  - Supports deterministic tamper detection (`snapshot verify`) and byte-for-byte restoration (`rollback <deploy-id>`).

- **Static CLI Distribution:**
  - Pure Go standard library (`CGO_ENABLED=0`, zero external dependencies) covering `daemon`, `catalog`, `deploy`, `rollout`, `rollback`, `snapshot`, `audit`, `status`, and `version`.
