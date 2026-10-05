# UPDATE.md — v1.8.6 Release Notes & Maintenance Behavior

**Release:** `v1.8.6` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ c7f335d` (`v1.8.5`) · **Date:** 2026-10-05
**Package:** `SHEYTAN-Local-Agent-v1.8.6-PHASE2-FINAL.zip` (complete repository
tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior.

## v1.8.6 changes (Phase 2: deep execution-engine / resource integration)

1. **P0 — execution truth: enumeration is selection evidence, never
   execution proof.** The ONE accelerator authority now enforces
   `GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
   verified` end-to-end: `--list-devices` enumeration selects GPU_VULKAN
   with the pending-execution verification plan and the CPU safety net;
   `ExecutionVerified` requires the measured runtime offload line or a
   still-valid **ExecutionReceipt** (a new structured identity-carrying
   object — engine tag, variant, device, model, status — whose
   `ValidFor(engineTag, variant)` check rejects receipts from another
   engine build, another variant or a failed bounded probe: stale
   evidence can never falsely verify a new engine). CPU stays verified
   by definition of execution.

2. **P0 — the GPU transaction is authoritative; premature activation
   removed.** A NORMAL serving launch enables `--n-gpu-layers 99` (AUTO)
   ONLY on proven execution — the current boot's measured offload line or
   a persisted verified GPU-probe receipt whose engine identity still
   matches. Enumeration alone and Vulkan DLL presence keep AUTO CPU-safe.
   The bounded candidate transaction may still boot Vulkan to PROVE it
   (a strictly transaction-scoped proving mode inside
   `updateEngineVariantTx`): commit still requires a real generation AND
   the measured offload line, failure rolls back to the known-good
   CPU/fallback with the truthful state — the v1.7.2 bounded
   `gpu-probe.json` behavior is preserved exactly. Manual `numGpu`
   configuration is respected verbatim; the CPU-forced profile is
   enforced at the launcher too. The offload evidence is PER-BOOT now: a
   restart or model swap re-proves (no inherited GPU claim).

3. **P0 — `/api/engine` and `/api/perf` agree.** One accessor serves both
   surfaces; the accelerator resolution memo records its input signature
   (engine tag + variant + requested profile + loaded model + offload
   evidence + probe state) and any stale memo is recomputed before a
   consumer can read it. The engine surface no longer depends on the
   performance page polling first; a background warm-up at server start
   keeps the first poll off the one-time enumeration cost; the execution
   stage cannot move backwards because a later UI poll happened — only
   because the serving reality itself changed.

4. **P0 — real resource integration through the ONE Governor.** The
   Governor's resource state now folds the CURRENT inference workload
   (model file bytes as a FILE fact + planned KV at the serving window,
   from the existing model-card/context authorities); the envelope
   accounts for a footprint that consumes the resident budget (background
   work reduced through the existing honest adjustment class, reason
   stated; RAM stays memory capacity, never an accelerator; unknown
   stays unknown). A resource-aware run gate consults the Governor's
   measured envelope BEFORE any engine start (EnsureLLM /
   EnsureLLMContext): sustained pressure defers the model load with the
   explainable reason; an unmeasured Governor falls through to the
   existing preflight gate exactly as before. The engine process RSS is
   now MEASURED (through the existing `resources.ProcRAM` authority) and
   flows into the Governor's engine facts; a new `/api/perf`
   `engineMemory` block serves the engine's real memory evidence with
   provenance labels — measured process RSS, the model FILE size
   (explicitly never "RAM used"), the runtime offload line, and the
   KV-cache allocation named UNKNOWN where the engine exposes no
   measured surface (never a guessed figure).

5. **P1 — Windows CPU telemetry + preserved truth surfaces.** The
   Governor's Windows CPU seam measures real load via kernel32
   `GetSystemTimes` through the one shared priming/delta state machine
   (first sample primes; deltas are real; failures stay unknown) — no
   second sampler, no second cadence; the Linux seam is untouched. The
   native C++ engine's capabilities stay explicit (CPU-only execution,
   no GPU claim, no faked reasoning budgets); the GPU backend direction
   remains the PROVEN llama.cpp Vulkan transaction (no SYCL/OpenVINO
   backend is claimed — the repository cannot yet provision, launch,
   test and verify one).

6. **Version identity.** All release surfaces at 1.8.6 through the ONE
   canonical gate (`node scripts/release-version.mjs --check`).

## v1.8.5 changes (Phase 1 of the staged engine program)

1. **P0 — live streaming, server side of the wire closed.** The v1.8.4
   frontend scheduler repaired the RENDER-side loss (triple-boundary,
   self-healing flush); the remaining bottleneck sat in the SERVER's run
   hub: a plain 128-deep channel whose overflow policy DROPPED THE NEWEST
   event. For cumulative response/reasoning snapshots that discards the
   frame carrying the FULL text while the buffer keeps stale prefixes —
   exactly the "streamed text visible only after Stop" class under a
   backpressured/stalled client transport. The hub now delivers through a
   bounded, conflation-aware queue: overflow evicts the OLDEST conflatable
   snapshot (newest-wins), order and the seq replay contract are
   preserved, terminal events are never preferentially evicted, the
   publisher never blocks, memory stays bounded, and every event write
   carries a generous deadline so a wedged client tears down
   deterministically and recovers through the reconnect snapshot replay.
   Deterministic hub/queue suites (9 tests) + the race gate; the full API
   suite stays green.

2. **P0 — the reasoning-depth ladder Low / Mid / High / Ultra (real
   backend meaning, end-to-end).** Each level carries a numeric
   thinking-token budget onto the generation request via the llama.cpp
   request-level `reasoning_budget_tokens` parameter — verified present
   in BOTH managed engine builds (b10642 default, b11205) by reading the
   actual server sources: low = 0 (thinking off, the engine's own
   "immediate end" semantics), mid = 1024 (bounded default), high = 4096,
   ultra = the engine default (no client-side cap — the honest encoding).
   Local engines only (the same gating as the other llama.cpp-specific
   request fields); remote providers never receive the field; a
   non-thinking model ignores it (reasoning is never fabricated); the
   native C++ path has no budget control and the level is documented-inert
   there. The wire/Go/store/front ladder is one contract — the level
   travels with EVERY run request; the legacy Auto/Fast/Thinking
   vocabulary migrates at both boundaries and is never re-emitted.

3. **P0 — Show Thinking / Hide Thinking (visibility only).** A persisted
   presentation preference that gates the RENDERING of backend-reported
   reasoning across the live generation bubble, the settled safety-net
   bubble and history messages. It never changes generation settings: the
   reasoning level and its budget travel with every request regardless,
   the store keeps folding reasoning snapshots, and unavailable reasoning
   stays unavailable. Chat and Agent share the semantics through the
   shared composer/message surfaces.

4. **P1 — the engine is supervised machinery.** The user-facing Start/Stop
   engine toggle is removed from the ordinary workflow. The engine boots
   on first use through the run gate (EnsureLLMContext), restarts through
   the settings flow after engine-affecting changes, and recovers through
   internal supervision; the UI represents engine state (runtime pill,
   badge, live phase). All internal lifecycle operations — startup,
   engine update, restart, recovery, shutdown — are unchanged.

5. **Phase 2 foundation — the execution/evidence ladder.** ONE shared
   structure (`internal/llm/execution.go`, surfaced as `/api/engine`'s
   `execution` block) composes the existing authorities into the monotone
   ladder detected → backend-available → device-selected → model-loaded →
   generation-executed → execution-evidence → verified. Device
   enumeration can never equal verified execution: the pure composer
   stops at the first unproven rung and names the gap. No new sampler, no
   second policy engine — inputs come from device detection, backend
   health, the accelerator selection memo, verified model loading,
   measured generation telemetry and runtime offload lines. The deep
   physical-GPU execution proof and the unified C++ execution path remain
   Phase 2 work (see `ROADMAP.md` §Phase 2).

6. **Version identity.** All release surfaces at 1.8.5 through the ONE
   canonical gate (`node scripts/release-version.mjs --check`).

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
* v1.8.5 evidence: the hub's drop-newest overflow policy is reproduced
  deterministically (a full, never-drained queue must still end on the
  NEWEST snapshot — the v1.2.x policy fails the test and the conflation
  queue passes); terminal-event survival, publish-never-blocks, close
  drains and eviction order are pinned the same way; the reasoning ladder
  is pinned at the wire field name, the budget numbers, the local/remote
  gating and the legacy migration (Go suite) AND at the payload/persistence
  split (store-level scripted-transport suite); the execution ladder's
  detected ≠ verified rule is pinned by the monotone-stage suites. The
  reasoning_budget_tokens parameter was verified against the ACTUAL
  server sources of both managed builds (b10642/b11205) — not against
  documentation alone.
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
