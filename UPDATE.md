# UPDATE.md — v1.8.4 Release Notes & Maintenance Behavior

**Release:** `v1.8.4` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ ca44b08` (`v1.8.3`) · **Date:** 2026-10-01
**Package:** `SHEYTAN-Local-Agent-v1.8.4-FINAL.zip` (complete repository
tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior.

## v1.8.4 changes

1. **P0 — live streaming visibility (response AND thinking) during an
   active run.** The v1.8.2 dual-boundary flush carried a latent wedge:
   the MessageChannel task controller was one-shot, and a post arriving
   while a message was still in flight chained onto the armed callback
   without posting a new one. One lost or indefinitely delayed MessageChannel
   delivery — the failure class the reported Windows runtime exhibited —
   left the flush latch pending forever; streamed text accumulated in the
   accumulator until Stop's synchronous flush revealed it. The v1.8.4
   scheduler arms THREE independent boundaries behind the ONE coalescing
   latch (reusable MessageChannel macrotask + 0ms timer task + animation
   frame); the task controller is recoverable by construction (latest-wins,
   bounded handshake, deterministic microtask fallback). The ACTIVITY flush
   (statuses, tool events, done/error/aborted) moved off its rAF-only
   schedule onto the same scheduler. Streaming efficiency is unchanged:
   cumulative snapshots, one coalescing latch, no per-token renders, no
   sleeps, no polling.

2. **P0 — AUTO Vulkan provisioning unblocked; explicit OFF stays
   respected; the engine-identity verification window is truthful.**
   (i) The stale derived CPU posture (`gpuAutoOffload=false` written by
   the pre-1.8.4 recommendation pipeline) no longer blocks the AUTO
   candidate: the recommendation never writes a derived OFF, the config
   records explicit user actions (`gpuAutoOffloadUserSet`), and Load
   repairs the legacy derived state once, with an honest note, persisted.
   Explicit user OFF, manual layer counts and the CPU requested profile
   are never touched. (ii) During the deferred-commit verification window
   the boot path now derives identity from the installer's staged marker
   and probes the ACTUAL swapped-in binary — "staged b11310, probe
   reports b11310" — instead of misreporting the previous recorded tag
   and reusing the old build's capability profile. Commit/Rollback clear
   the marker; the transaction's stop → stage → verify → start → health
   → execution-evidence → commit/rollback choreography is unchanged.

3. **P0 — zero-session Send.** Deleting the final session leaves a valid
   zero-session state, the composer stays usable, and pressing Send
   automatically creates + activates a session in the current mode and
   continues the same run (the store's lazy creation, now reachable).
   Chat and Agent modes behave identically; the created session persists
   across reloads.

4. **P0 — monotonic context refresh.** The context UI can no longer show
   stale usage: every context refresh carries a monotonic generation; only
   the newest request for the still-active session may write
   `sessionContext`. Session change, deletion (cleared + replacement
   refreshed), creation, mode switch and fresh-run transitions invalidate
   in-flight responses. The UX remains automatic/unlimited; physical
   limits remain governed by model + engine + runtime (the backend stays
   the ONE context authority).

5. **P1 — config-write atomicity.** Every config write uses a unique
   same-directory temp file (the fixed `config.json.tmp` name was a
   latent cross-writer race, observed as a hard engine-start failure in
   this repo's own verification funnel).

## Maintenance / update / rollback behavior (current)

* The maintenance gate (identity manifest + committed state) still owns
  engine updates; two-boot idempotency and the corruption matrix are
  unchanged (deterministic, fault-injected).
* During a deferred engine update the serving binary is the byte-verified
  staged candidate and the staged-identity marker (`engine-stage-pending.json`,
  inside the managed bin directory) is its identity authority until
  Commit records the new tag and removes the marker. Rollback restores
  the previous package and the marker is gone with the staged tree.
* The v1.8.4 GPU-posture repair is a ONE-TIME config migration: it fires
  at Load only for a marker-less derived CPU posture under a non-CPU
  requested profile, repairs to the AUTO default, appends an honest
  PathNote and persists. After the repair it can never fire again. There
  is no other data migration of any kind: the session store's on-disk
  format, the index and the sidecars are byte-compatible with v1.8.3.
* The update path still resolves engine variants through the ONE
  authoritative resolver (pinned tag first, else newest release containing
  the asset); unsupported variants are REFUSED, never silently CPU-fallen.
* The AUTO Vulkan candidate evidence ladder is unchanged: supported
  variant → resolved asset → staged package → byte identity → start →
  health → device enumeration → bounded real generation → offload-line
  evidence → commit (or rollback). Detection alone is never proof.
* The Runtime Governor adds NO persistence, NO new scheduler and NO new
  updater: it is a policy read-model over existing telemetry. Restarting
  the app re-measures; nothing to migrate.

## Evidence-truth statements (standing)

* Deterministic unit / race / integration / E2E / CI / real-engine probe /
  real-host runtime are DISTINCT evidence classes and are never conflated.
* v1.8.4 evidence: the flush-scheduler wedge is reproduced deterministically
  at the unit level (a lost channel message must self-heal — the design
  fails the test before the repair and passes after); the context races
  are forced by causality over a scripted transport; zero-session Send is
  proven store-level AND through the real stack in a browser (including
  visible-before-completion with requestAnimationFrame suspended).
* Vulkan GPU execution is NOT claimed by this release: the AUTO path and
  the identity window are repaired and deterministically tested, but a
  real-GPU offload claim still requires the physical execution evidence
  the transaction itself collects — CI and this repository cannot
  manufacture it.
* Build/typecheck ≠ runtime proof; CI ≠ physical-host runtime proof;
  detection ≠ execution. The physical-Windows acceptance walkthrough
  (launch, delete the final session, Send with zero sessions, live
  streaming during generation, reload persistence) remains the user's own
  acceptance step — no physical-PC claim is made by CI or by this release.

---

# Historical records (compressed — authoritative detail lives in the tags, their tests, and `changelog.md`)

## v1.8.3 record

The Linux CI session-delete failure root-caused with instrumented
evidence (measurement race in the test, not the product); the startup
session-list write moved through the ONE generation guard; honest DELETE
errors + idempotent create-prepend; deterministic three-layer
session-delete coverage; ROADMAP evidence-ranking.

## v1.8.2 record

The "text visible only after Stop" live-rendering defect repaired by the
dual-boundary flush (event-loop task + animation frame, one coalescing
latch); factual no-reasoning-stream state; backend-truth memory evidence
composed at the injection site; the deterministic capability self-model;
the (path,size,mtime) model-card cache; central log redaction.

## v1.8.1 record

The real-Windows local-generation crash repaired at the root (Gemma-class
projection-tensor quantization mismatch: verified before download,
refused with the exact mismatch named); engine-build identity contract.

## v1.8.0 record

Runtime Governor: resource state, envelopes, admission, hysteresis,
self-model, `/api/governor`; cooperative protection unchanged.

## v1.7.6 record

Continuum (chapter rollover) + vision-projector honesty surfaces.

## v1.7.5 record

Session-list generation guard (refreshSessions consumer), run-control
E2E, stale-run protection hardening.

## v1.7.4 record

Pause/edit/resume (durable checkpoints, revision conflicts), live
streaming fast path foundations.

## v1.7.0–v1.7.3 record

Scheduler RunNow settlement contract (close = persistence + bookkeeping
complete); recovery/handoff durability; engine-lifecycle and stress
hardening.

## v1.7.1 record

Preflight gate: one report over existing authorities, hard
incompatibilities refuse runs BEFORE engine start; LiveMonitor with
hysteresis and synchronous cooperative critical protection (active run
contexts canceled — nothing killed); context-exhaustion recovery (typed
conditions only, exactly one bounded restart); native engine first-class
backend; license/doc cleanup.

## v1.6.x record

Engine-variant provisioning through the authoritative resolver with
Windows-runner asset validation (HEAD-checked reachability ≠ runtime
execution — labeled then and now); stable embedded-frontend asset
contract; custom tools surface; workflow output-name repair.

## v1.5 and earlier (compressed)

Multi-agent context, memory/recall tiers, durable summaries, cross-mode
history, task state, agent.md handoff, continuum rollover, repository
index, Lab workspaces, downloader/installer foundations, environment
centre, streaming replay contracts — each with the deterministic and
integration evidence that shipped in its release.
