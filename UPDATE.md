# UPDATE.md — v1.9.0 Release Notes & Maintenance Behavior

**Release:** `v1.9.0` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 0785d7d` (`v1.8.8`) · **Date:** 2026-10-08
**Package:** `SHEYTAN-Local-Agent-v1.9.0-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior.

## What v1.9.0 adds

1. **AI Systems — the first-class, user-owned run configuration.** Create,
   edit, activate, clone, export, import and delete AI Systems from the
   System Centre (`/api/systems` on the backend). The ACTIVE system binds
   every NEXT run: instructions, model override, reasoning effort
   (low/mid/high/ultra on the real budget ladder), the allowed tool
   surface (server-side, remove-only), the skills surface, and the
   approval + verification policies. Every run freezes a snapshot
   (`systemId` + `systemRevision`) before it starts — editing a system
   mid-run never mutates a running configuration. A fresh install gets
   exactly one **Default** system whose behavior is identical to the
   pre-1.9 product; upgrading is non-destructive.

2. **Goals — durable long-horizon work.** Describe a long-horizon
   objective in the Goals card (or `POST /api/goals`); the engine drives
   understanding → planning → acting → verifying → completed through the
   ONE orchestrator, checkpointing after planning and after every
   completed step. Goals pause, resume from their checkpoint without
   replaying committed mutations, park when awaiting approval, block
   honestly on recoverable failures (with bounded replanning), and
   settle terminally ONLY on objective verification evidence. After a
   restart, any goal found live with no run behind it is marked paused —
   never falsely running.

3. **Approvals — one deterministic risk vocabulary.** Every tool call
   classifies as read-only, workspace-write, external-network,
   destructive or privileged/host-level. Goal runs deny (never silently
   execute) calls whose risk class requires approval under the AI
   System's policy; pending approvals persist durably, approval resumes
   the EXACT call, rejection is recorded as evidence, and stale approval
   ids are rejected. Chat and Agent runs behave exactly as before.

4. **Bounded delegation.** `internal/multiagent/subtasks` executes
   bounded subtasks beside the advisory specialists: read-only
   parallelism at most two, mutating work serialized, no nested
   spawning, deadlines that block honestly, deterministic
   subtaskId-order merge, and failures that stay failures.

5. **Repository navigation (`repo_nav`).** After `repo_search` locates
   evidence, `repo_nav` opens, navigates, reads exact ranges and greps
   inside identified files — bounded, provenance-tagged, truncation-
   honest, and path-safe (traversal, absolute, volume and UNC forms are
   refused identically on Linux and Windows).

6. **P0 fixed — the zero-session reload defect.** The v1.8.8 Linux
   Browser-E2E blocker is root-caused and repaired: the run-startup
   state now precedes every transport step (including the lazy session
   create), failures clean up deterministically, and the E2E waits for
   the deterministic run-dispatch marker plus the durable post-reload
   transcript. Deterministic store-level regressions pin the contract.

## Maintenance behavior (unchanged where not stated)

* **Update flow** — the existing updater/downloader authority checks,
  stages and installs releases; staging keeps a resumable `.part` file;
  cancel stops the download without deleting it.
* **Data locations** — user config remains `config.json` under the
  canonical data root; new durable stores live beside the existing ones:
  `<data>/ai-systems/`, `<data>/goals/`. Deleting an AI System document
  or goal document by hand is tolerated (the stores are corruption-
  tolerant and repair the active pointer to Default).
* **Default AI System** — reserved id `default`; it cannot be deleted;
  deleting the ACTIVE system falls activation back to Default. Its
  behavior is the pre-1.9 runtime behavior (no instructions, no
  overrides).
* **Engine/execution evidence** — unchanged from v1.8: detection ≠
  availability ≠ selection ≠ execution ≠ verification; identity-bound
  execution receipts; one accelerator authority; one Governor.
* **Frontend verification stack** — typecheck, oxlint, the node unit
  suite (`npm run test:units`, includes the v1.9 view-model tests), and
  the release gates (`npm run test:release`) all run in `--check` mode
  green on this release.
* **Browser E2E** — `e2e/ai-systems.spec.ts` joins the suite: the
  default-system-first contract, create+activate, activation surviving a
  reload, and the goal create→cancel lifecycle against the real headless
  server + engine fixture.

## Known boundaries (honest)

* The goal runner does not yet decompose steps into subtasks
  automatically (the delegation engine is implemented and tested; the
  integration is the first v1.9.x work item).
* The protected-evaluation anti-hack guard is designed but not
  implemented; until it lands, protected evaluation hygiene relies on
  the existing Lab policy and workspace boundaries.
* The MCP client remains implemented and tested but is not yet
  registered into the runtime tool registry.
