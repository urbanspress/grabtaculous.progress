# HANDOFF.md — Grabtaculous

- **Current Owner:** Google Antigravity (`Google Hydrogen (XHigh)` — Bootstrap & `v0.1.0` Specification Drafted; awaiting explicit project-owner approval before any implementation code is written)
- **Current Milestone State:** **`v0.1.0` — `Specification in Review`** ([`specifications/GRABTACULOUS-SPEC-001.md`](specifications/GRABTACULOUS-SPEC-001.md)) — Zero Go application code written yet per Spec-First Approval Gate.
- **Next Action:** Project owner reviews `specifications/GRABTACULOUS-SPEC-001.md` (alongside `GRABMAN-SPEC-001.md` and `GASHBOARD-SPEC-001.md`).

---

## Frozen Upstream Contracts (Unmodified)
- **GRAB:** `v1.1.0` (`urbanspress/grab`)
- **Grex JS:** `v1.0.0` (`urbanspress/grex-js`)
- **GrabLab CMS:** `v1.0.1` (`urbanspress/grablab-cms`)
- **Grex Theme Builder:** `v1.0.0` (`urbanspress/grex-builder`)
- **Groxy AI:** `v1.0.0` (`urbanspress/groxy-ai`)

---

## Bootstrap Completion Log (Pre-Implementation)
1. Initialized twin GitHub repositories: `urbanspress/grabtaculous.progress` (`origin`) and `urbanspress/grabtaculous.stable` (`stable`).
2. Added governance and documentation baseline:
   - `AGENTS.md` (4-tier model routing, Mandatory Pause & Swap Rule, `sync-notebook-sources.ps1` rule, SemVer + 6-Layer Ecosystem Compatibility Matrix)
   - `GRABTACULOUS-EXPERT.md`
   - `docs/ROADMAP.md`
   - `README.md` with committed emerald `#10B981` SVG badges (`docs/badges/*.svg`)
   - `LICENSE` (MIT) and `.gitignore`
3. Drafted [`specifications/GRABTACULOUS-SPEC-001.md`](specifications/GRABTACULOUS-SPEC-001.md) covering the multi-server supervisor daemon, `--read-only` container orchestrator, curated 1-click app marketplace catalog, zero-downtime `Grisp` rollouts, and pre-deploy snapshot rollbacks.

---

## Next Step Model Guidance
- **Recommended Model for Reviewing/Refining Spec:** **`Google Hydrogen (Medium)`** if you want edits or refinements to `GRABTACULOUS-SPEC-001.md`.
- **Recommended Model Once Spec Is Approved:** **`Google Hydrogen (Extra)`** to implement Grabtaculous's multi-server orchestration, `--read-only` container generator, zero-downtime `Grisp` rollout controller, and snapshot rollback state machine, pausing before pre-`v1.0.0` security & rollout audits to switch to **`Orchestrator (Argon)`** for an independent second-opinion audit.
