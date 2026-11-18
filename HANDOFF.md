# HANDOFF.md — Grabtaculous

- **Current Owner:** Google Antigravity (`grabtaculous v1.1.1` Complete — Synchronized across `origin` and `stable`)
- **Current Milestone State:**
  - **`v0.1.0` — `Complete`** ([`specifications/GRABTACULOUS-SPEC-001.md`](specifications/GRABTACULOUS-SPEC-001.md))
  - **`v1.0.0` — `Complete`** ([`specifications/GRABTACULOUS-SPEC-100.md`](specifications/GRABTACULOUS-SPEC-100.md), [`docs/ROADMAP.md`](docs/ROADMAP.md))
  - **`v1.1.0` — `Complete`** ([`specifications/GRABTACULOUS-SPEC-110.md`](specifications/GRABTACULOUS-SPEC-110.md), [`vps-starter-kit.md`](vps-starter-kit.md)): Official `nginx.org` NGINX `1.30.6` (HTTP/3, `aio threads;`, `/_protected_media/` + `/_protected_b2/` `X-Accel-Redirect`, fail-closed `default_server` `444` / `ssl_reject_handshake on;` `grisp.conf`, isolated `wordpress-fpm.conf`), hardware profiles (`vps-6c-12g` through `vps-16c-64g`), `>= 15 GB` NVMe reserve guard, Contabo -> Bluehost migration engine (`migrate-export` / `migrate-import`), and curated `searxng` & `wordpress-fpm` blueprints.
  - **`v1.1.1` (`PATCH 1.1.x`) — `Complete`** ([`specifications/GRABTACULOUS-SPEC-110.md`](specifications/GRABTACULOUS-SPEC-110.md), [`VPS-DEPLOY-TEST-FIRST.md`](VPS-DEPLOY-TEST-FIRST.md)): Universal zero-shell native VPS hardware auto-detection (`grabtaculous profile detect`, `--profile auto` for `profile render` & `edge render`), mandatory 5-Gate `VPS-DEPLOY-TEST-FIRST.md` verification protocol (verified live on Contabo `vps-6c-12g`), `GRAB-E210` StrictJSON & loopback-only `allowed_cidrs` (`["127.0.0.1/32", "::1/128"]`) passing `grabman doctor --strict` + `grab check`, and reusable `grisp-locations.inc` / `grisp-domain.conf.template`.
- **Pending `v1.1.1` Tasks:** None (`100%` Complete).
- **Release Readiness:** `Grabtaculous v1.1.1` (`PATCH 1.1.x`) is `100%` drop-in backwards compatible with `v1.1.0`, `v1.0.0`, **grabman v1.0.0–v1.1.0**, **Gashboard v1.0.0**, and **Groxy Desk v1.0.1**.

---

## `v1.1.1` Verification & Release Summary
- **Pure Go Stdlib / Zero External Dependencies (`CGO_ENABLED=0`):**
  - `go build ./...`: `PASS` (clean)
  - `go vet ./...` (`windows/amd64` + `GOOS=linux GOARCH=amd64`): `PASS` (clean)
  - `go test -count=1 ./...`: `PASS` (`100%` pass across zero-shell native hardware auto-detection, `--profile auto` rendering across all 5 tiers `vps-6c-12g`..`vps-16c-64g`, `GRAB-E210` StrictJSON + loopback `allowed_cidrs`, `grabman doctor --strict` + `grab check` parity, NGINX `1.30.6` edge templates, 15 GB NVMe reserve guard, migration bundles, loopback daemon, and snapshot rollback)
  - `go mod verify`: `all modules verified`
  - `govulncheck ./...`: `No vulnerabilities found.`
- **Deliverables & Static Release Assets (`dist/`):**
  - `grabtaculous-windows-amd64.exe` (`windows/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-amd64` (`linux/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-arm64` (`linux/arm64`, `CGO_ENABLED=0`)
  - `Grabtaculous-v1.1.1-Guide.pdf` (`docs/releases/pdf/Grabtaculous-v1.1.1-Guide.pdf`)
  - `grabtaculous-v1.1.1-bundle.zip` + `SHA256SUMS.txt`

---

## `v1.1.0` Verification & Release Summary
- **Pure Go Stdlib / Zero External Dependencies (`CGO_ENABLED=0`):**
  - `go build ./...`: `PASS` (clean)
  - `go vet ./...`: `PASS` (clean)
  - `go test -count=1 ./...`: `PASS` (`100%` pass across NGINX `1.30.6` bootstrapper & HTTP/3 + `X-Accel-Redirect` generator, all 5 VPS hardware profiles `vps-6c-12g`..`vps-16c-64g`, `>= 15 GB` NVMe reserve guard, `migrate-export` / `migrate-import` tamper & secret exclusion, curated 12-blueprint catalog including `searxng` and `wordpress-fpm`, stealth loopback daemon, `/_ready` rollouts, and snapshot rollback)
  - `go mod verify`: `all modules verified`
  - `govulncheck ./...`: `No vulnerabilities found.`
- **Deliverables & Static Release Assets (`dist/`):**
  - `grabtaculous-windows-amd64.exe` (`windows/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-amd64` (`linux/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-arm64` (`linux/arm64`, `CGO_ENABLED=0`)
  - `Grabtaculous-v1.1.0-Guide.pdf` (`docs/releases/pdf/Grabtaculous-v1.1.0-Guide.pdf`)
  - `grabtaculous-v1.1.0-bundle.zip` + `SHA256SUMS.txt`

---

## `v1.0.0` Verification & Release Summary
- **Pure Go Stdlib / Zero External Dependencies (`CGO_ENABLED=0`, `go1.27.4`):**
  - `go build ./...`: `PASS` (clean)
  - `go vet ./...`: `PASS` (clean)
  - `go test -count=1 ./...`: `PASS` (`100%` pass across container hardening, edge proxies, SHA-256 catalog tamper detection, stealth loopback daemon + CLI end-to-end, `/_ready`-gated zero-downtime `Grisp` rolling swaps, live `GRAB v1.1.0` + `grabman v1.0.0` preflight/migrate/auto-rollback, and Pre-Deploy Snapshot restore)
  - `go mod verify`: `all modules verified`
  - `govulncheck ./...` (`go1.27.4`): `No vulnerabilities found.`
- **Deliverables & Static Release Assets:**
  - `grabtaculous-windows-amd64.exe` (`windows/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-amd64` (`linux/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-arm64` (`linux/arm64`, `CGO_ENABLED=0`)
  - `Grabtaculous-v1.0.0-Guide.pdf` (`docs/releases/pdf/Grabtaculous-v1.0.0-Guide.pdf`)
  - `grabtaculous-v1.0.0-bundle.zip` + `SHA256SUMS.txt`
- **Next Action & Recommended Model for Step 6 (`Groxy Desk`):**
  - **Next Step:** Ready for **Gashboard v1.0.0** integration & **Step 6 (`Groxy Desk`)** planning.
  - **Recommended Antigravity Model:** Use **`Google Hydrogen (Medium)`** for Step 6 (`Groxy Desk`) specification/design docs, **`Google Hydrogen (High)`** for single-module implementation, or **`Orchestrator (Argon)`** for multi-repo Step 6 cross-layer wiring.

---

## `v0.1.0` Verification & Release Summary (Historical)
- **Pure Go Stdlib / Zero External Dependencies (`CGO_ENABLED=0`):**
  - `go build ./...`: `PASS` (clean)
  - `go vet ./...`: `PASS` (clean)
  - `go test -count=1 ./...`: `PASS` (`100%` pass across container hardening, edge proxies, SHA-256 catalog tamper detection, stealth loopback daemon + CLI end-to-end, `/_ready`-gated zero-downtime `Grisp` rolling swaps, live `GRAB v1.1.0` + `grabman v0.1.0` preflight/migrate/auto-rollback, and Pre-Deploy Snapshot restore)
  - `go mod verify`: `all modules verified`
  - `govulncheck ./...` (`go1.27.4`): `No vulnerabilities found.`
- **Deliverables & Static Release Assets:**
  - `grabtaculous-windows-amd64.exe` (`windows/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-amd64` (`linux/amd64`, `CGO_ENABLED=0`)
  - `grabtaculous-linux-arm64` (`linux/arm64`, `CGO_ENABLED=0`)
  - `Grabtaculous-v0.1.0-Guide.pdf` (`docs/releases/pdf/Grabtaculous-v0.1.0-Guide.pdf`)
  - `grabtaculous-v0.1.0-bundle.zip` + `SHA256SUMS.txt`
- **Next Action & Recommended Model After Spec Approval:**
  - **Pause Gate (`v1.0.0`):** Awaiting explicit project-owner approval of [`specifications/GRABTACULOUS-SPEC-100.md`](specifications/GRABTACULOUS-SPEC-100.md) before freezing `v1.0.0` runtime strings, creating `v1.0.0` tags, or promoting to `stable`.
  - **Recommended Antigravity Model:** Use **`Google Hydrogen (High)`** for single-repo `v1.0.0` freeze/build verification, or **`Orchestrator (Argon)`** when coordinating the synchronized 3-repo Step 5 `v1.0.0` release across `grabman`, `grabtaculous`, and `gashboard`.
