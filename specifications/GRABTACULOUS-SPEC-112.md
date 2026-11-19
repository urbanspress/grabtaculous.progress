# GRABTACULOUS-SPEC-112: Grabtaculous `v1.1.2` — WordPress 7.1.5 LTS Baseline Bump & Configurable `WP_IMAGE` Override in Catalog Blueprint

- **Status:** Complete (Approved by project owner; implemented and verified in `v1.1.2`)
- **Target Release:** `v1.1.2` (`PATCH 1.1.x` — Non-Breaking Catalog & Playbook Bump)
- **Foundational Baselines (Frozen `v1.x` Contracts Preserved):**
  - **GRAB:** `v1.1.0` (Frozen)
  - **Grex JS:** `v1.0.0` (Frozen)
  - **GrabLab CMS:** `v1.0.1` – `v1.4.0` (Frozen)
  - **Grex Theme Builder:** `v1.0.0` (Frozen)
  - **Groxy AI:** `v1.0.0` – `v1.8.0` (Frozen)
  - **Grabman:** `v1.0.0` (Frozen)
  - **Grabtaculous:** `v1.1.1` (Current baseline passing all 5 gates)
- **Governance:** Read alongside [`GRABTACULOUS-EXPERT.md`](../GRABTACULOUS-EXPERT.md), [`AGENTS.md`](../AGENTS.md), [`specifications/GRABTACULOUS-SPEC-110.md`](GRABTACULOUS-SPEC-110.md), and [`docs/ROADMAP.md`](../docs/ROADMAP.md).

---

## 1. Executive Summary (Plain English)

In production deployments, the Grabtaculous curated companion catalog provides one-click, hardened, `--read-only` application blueprints. The existing `wordpress-fpm` blueprint (`catalog/companions/wordpress-fpm.json`) was originally authored with WordPress `6.8.1` as its static default image.

With the official release of the **WordPress 7.1.x series (active security release 7.1.5)**, operators deploying new companion WordPress testing and blog sites need:
1. **Fresh Out-of-the-Box Baseline:** New deployments must pull and install **WordPress 7.1.5 LTS (`wordpress:7.1-fpm-alpine`)** instead of the legacy 6.8 series.
2. **Flexible `WP_IMAGE` Override:** To satisfy the requirement that operators can choose to always track the latest release without waiting for a new Grabtaculous release, the blueprint will support an optional `WP_IMAGE` variable in `.env` (e.g., `WP_IMAGE=wordpress:fpm-alpine`), while preserving a verified, pinned `@sha256:...` digest as the safe default.

This specification defines the exact, non-breaking catalog updates and playbook documentation synchronization for **Grabtaculous `v1.1.2`**.

---

## 2. Proposed Changes

### 2.1 Catalog Blueprint: `catalog/companions/wordpress-fpm.json`
1. Update `version` from `"6.8.1"` to `"7.1.5"`.
2. Add optional variable `WP_IMAGE` to `required_variables`:
   ```json
   {
     "name": "WP_IMAGE",
     "description": "WordPress container image (defaults to verified WordPress 7.1.5 LTS FPM Alpine)",
     "required": false,
     "default": "docker.io/library/wordpress:7.1-fpm-alpine@sha256:74fd3fffc4b3b0c40d4cc0803ec40fe1a7febf0437e96d55b9cbc9a4e54a1aae"
   }
   ```
3. Update `services[0].image` to support the override with pinned fallback:
   ```json
   "image": "${WP_IMAGE:-docker.io/library/wordpress:7.1-fpm-alpine@sha256:74fd3fffc4b3b0c40d4cc0803ec40fe1a7febf0437e96d55b9cbc9a4e54a1aae}"
   ```
4. Keep all existing `--read-only`, non-root UID `65532:65532`, `cap_drop: ["ALL"]`, `no-new-privileges:true`, and loopback `127.0.0.1:9000` FastCGI bindings completely intact.

### 2.2 Playbook Synchronization: `vps-starter-kit.md` & `docs/vps-starter-kit.md`
- Synchronize references from WordPress 6.8.1 to WordPress 7.1.5 LTS.

---

## 3. Verification & Safety Gates

1. Run `go test -count=1 ./...` (must pass 100%).
2. Verify catalog JSON parses cleanly and adheres to schema (`catalog/companions/wordpress-fpm.json`).
3. Confirm zero breaking changes to existing GrabLab CMS, Grisp supervisor, edge proxy, or hardware profile logic.
