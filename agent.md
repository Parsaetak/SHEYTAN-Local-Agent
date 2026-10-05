# SHEYTAN-Local-Agent — Agent Context (CURRENT v1.8.5 handoff)

This is the concise, current handoff for an engineering agent continuing
work on SHEYTAN-LA. It states what IS (verified), what is NOT (future), the
durable invariants, and the surfaces most sensitive to regression. Full
truth: `ARCHITECTURE.md` (architecture), `ROADMAP.md` (future),
`UPDATE.md` (release evidence), `worklog.md` (session log).

**Current release: v1.8.5 = PHASE 1 of the staged engine program
(COMPLETE).** Version identity is exactly `1.8.5` everywhere (canonical
gate: `node scripts/release-version.mjs --check`). Release history lives
ONLY in `changelog.md` (the README is current-only).

**THE NEXT PROGRAM IS PHASE 2 — deep execution-engine / resource
integration.** Its full scope is `ROADMAP.md` §Phase 2 (read that section
first): finish the unified C++ execution path behind the v1.8.5 execution
ladder, mature llama.cpp/ggml integration, real coordinated CPU/GPU
execution and model placement where supported, real memory/KV accounting,
backend selection from PROVEN runtime evidence (consume
`llm.ExecutionReport`'s verified rung — never detection posture), physical
GPU execution proof through the existing identity transaction, stronger
fallback/recovery, and the measured performance foundations (prefill,
cache reuse, TTFT, wire efficiency, Windows CPU telemetry through the
EXISTING Governor seam). Phase 3+ (AI System Builder, computer-use,
evaluation, routing) stays out of scope until Phase 2 lands.

---

## What SHEYTAN is

A Windows-first, local-first AI engineering laboratory — NOT an Ollama/LM
Studio wrapper. One Go runtime owns every authority exactly once; a
React/TypeScript frontend is embedded; managed llama.cpp and a native C++
engine path serve inference behind one backend contract. Motto: "The model
proposes. The tools execute. The laboratory verifies."

## Verified architecture (current)

* **One run lifecycle** — one registry, one hub, one `runLive` per run;
  sequence-stamped activity replay; settlement ordering (outcome recorded
  before the terminal flip; the settlement edge closes AFTER the registry
  record).
* **Pause / edit / resume** — durable atomic checkpoints; transactional
  edits with journals + startup recovery; resume = semantic continuation
  rebuilt from the transcript authority; ONE settlement path
  (`finishSuccessfulRun`) for fresh and resumed runs.
* **v1.8.0 synchronization contract** — after a resume, proof of a live
  resumed generation = strictly newer run sequence AND changed cumulative
  response/reasoning snapshot (`resumedGenerationEvidence`); stale
  cumulative state is never evidence; `waitForRunResponse` is
  initial-generation only (hazard note in source).
* **v1.8.0 abort honesty** — the orchestrator's context-cancelation exits
  publish `aborted` (never `done`); `observe()` folds it; live state and
  outcome registry agree; the frontend consumes the typed marker.
* **v1.8.1 local-generation crash repair** — a GGUF card that cannot be
  read (Gemma-class tokenizer blocks beyond the old 8 MiB bound, or any
  unreadable file) resolves to a nil capability object; every consumer
  follows the documented fallback (configured context + conservative
  estimator) — the orchestrator never dereferences it. The read bound is
  now 32 MiB, so real Gemma-class cards parse and drive the model-aware
  context clamp. FIRST local-provider run-level E2E tests exist in
  `internal/api` (fake llama-server subprocess, real engine contract).
* **v1.8.2 dual-boundary streaming flush → v1.8.4 TRIPLE-BOUNDARY
  SELF-HEALING FLUSH → v1.8.5 SERVER-SIDE CONFLATION DELIVERY** —
  `src/stream-fast-path.ts` still folds stream-critical WS events into
  the streaming accumulator at receive time;
  `src/stream-flush-scheduler.ts` arms THREE independent boundaries (a
  REUSABLE MessageChannel macrotask, a 0ms timer task, an animation
  frame) behind ONE coalescing latch, and the task controller is
  recoverable: latest-callback-wins + in-flight handshake + a
  deterministic microtask fallback whenever a post arrives while a
  message is still undelivered. The ACTIVITY flush uses the same
  scheduler; done/error/abort still flush synchronously. v1.8.5 closed
  the SERVER half of the same failure class: the run hub's subscriber
  delivery is a bounded, conflation-aware queue (`activitySub` in
  `internal/api/server.go`) — overflow evicts the OLDEST cumulative
  snapshot (newest-wins) instead of dropping the NEWEST event (the
  v1.2.x policy froze visible text at a stale prefix under transport
  backpressure — text appeared only after Stop, via the reconnect
  replay). Terminal events are never preferentially evicted; the
  publisher never blocks; event writes carry a generous deadline so a
  wedged client tears down deterministically. Regression suites:
  `internal/api/hub_conflation_v185_test.go` (9 tests, race-gated).
* **v1.8.4 zero-session Send** — a zero-session space is first-class:
  the composer stays usable and pressing Send runs the store's lazy
  `createSession()` in the current mode and continues the same run
  (`src/zero-session-send.test.ts` + `e2e/zero-session.spec.ts`).
* **v1.8.4 context-refresh generation** — `sessionContext` is written
  only by the newest context request for the still-active session; every
  session/mode/run transition invalidates in-flight responses
  (`src/context-refresh-race.test.ts` forces the interleavings by
  causality). The backend stays the ONE context authority; the UX stays
  automatic/unlimited.
* **v1.8.4 GPU posture + staged identity** — the recommendation never
  writes a derived `gpuAutoOffload=false`; explicit user OFF is marked by
  `gpuAutoOffloadUserSet` and respected; legacy derived CPU posture is
  repaired ONCE at Load (noted, persisted). The deferred-commit
  verification window keys boot identity off the installer's staged
  marker (`engine-stage-pending.json`) — the boot probe probes and
  reports the byte-verified staged binary, never a stale recorded tag
  (`internal/updater/staged_identity_v184_test.go`).
* **v1.8.5 REASONING-DEPTH LADDER (P0, real backend meaning)** — the
  composer control is low/mid/high/ultra; EVERY run request carries the
  level and its numeric thinking-token budget lands on the generation
  request (`agent.applyReasoningBudget` →
  `llm.ChatRequest.ReasoningBudget` → the llama.cpp request-level
  `reasoning_budget_tokens` parameter, VERIFIED against both managed
  builds' actual server sources: b10642 AND b11205). low=0 (thinking
  off), mid=1024 (default), high=4096, ultra=NOT SENT (engine default).
  Local engines only; non-thinking models ignore it; the native path is
  documented-inert. Legacy auto/fast/thinking migrate at BOTH the wire
  (`agent.NormalizeThinkingControl`) and persistence
  (`normalizeThinkingControl` in `src/run-events.ts`) boundaries —
  never re-emitted. Tier posture: low = latency floor, mid = neutral,
  high/ultra = thinking nudge WITHOUT context inflation (depth is the
  budget, not the tier). Suites:
  `internal/agent/reasoning_budget_v185_test.go` (9 tests) +
  `src/reasoning-controls.test.ts` (4 store-level suites).
* **v1.8.5 SHOW/HIDE THINKING (P0, visibility only)** — the persisted
  `showThinking` preference gates ONLY the rendering of backend-reported
  reasoning (live bubble, safety-net bubble, history messages —
  `src/MessageStream.tsx`); it never enters the payload (pinned), never
  changes the level/budget, and the store keeps folding reasoning
  snapshots while hidden. Chat and Agent share the semantics.
* **v1.8.5 ENGINE = SUPERVISED MACHINERY (P1)** — the user-facing
  Start/Stop engine toggle is REMOVED from `src/AgentBody.tsx`; the
  engine boots on first use (run gate `EnsureLLMContext`), restarts via
  the settings flow, recovers via internal supervision; the UI
  represents state. Internal lifecycle operations are all intact.
* **v1.8.5 EXECUTION/EVIDENCE LADDER (the Phase 2 boundary contract)** —
  `internal/llm/execution.go` + the `execution` block of
  `/api/engine`: a PURE composer over the existing authorities produces
  the monotone stage detected → backend-available → device-selected →
  model-loaded → generation-executed → execution-evidence → verified.
  Detection can never equal verified execution; gaps are named;
  unknowns stay unknown. The accelerator selection memo
  (`Server.lastResolution`, written by the ONE selection authority on
  /api/perf) feeds the device-selected rung — it is a read-through
  cache, NOT a second policy engine. Suites:
  `internal/llm/execution_v185_test.go` (5 tests).
* **v1.8.4 config-write atomicity** — every config write uses a unique
  same-directory temp file (the fixed `config.json.tmp` name raced
  concurrent writers into hard engine-start failures).
* **v1.8.2 memory evidence** — `contextplan.MemoryEvidence` rides the
  `context` activity; the live bubble renders exactly it
  (`src/memory-evidence.ts`). No fabricated counts, ever.
* **v1.8.2 capability self-model** — `taskclassify.SelfDescribe` intent +
  `internal/agent/selfmodel.go` (ONE formatter over the existing
  authorities); capability questions stay cheap.
* **v1.8.2 identity-based caps cache** — `internal/llm/modelcaps.go`
  caches the immutable GGUF card under (path, size, mtime) with no TTL;
  config-sensitive fields re-derive on a config fingerprint change.
* **v1.8.2 log redaction** — `internal/logging/redact.go` strips
  `runId=`/`session=`-style tokens at the central sink only.
* **v1.8.3 session-list generation authority (all consumers)** —
  `refreshSessions` AND the startup `initializeAgentOnce` write the
  session list through the ONE `sessionListGuard` ticket; superseded
  startup responses delegate to `refreshSessions()` instead of landing.
  `deleteSession` surfaces server failures honestly (store error state,
  no fake success); `createSession`'s prepend is idempotent by id
  (a refresh landing between POST dispatch and response cannot
  duplicate the row). Sidebar rows carry `data-session-id` for
  unambiguous identity (the rendered 8-char id slice collides within a
  bucket).
* **Runtime Governor** (`internal/governor`) — the ONE runtime POLICY
  authority: resource state (measured + explicit unknowns), sustained +
  rolling pressure signals over the shipped four-level vocabulary,
  envelopes with honest adjustment classes (`live`/`next-run`/`reload`),
  resident-budget admission (unknown evidence → conservative refusal),
  self-model with provenance. It executes NOTHING: the monitor keeps
  critical protection (cooperative cancellation), the engine lifecycle
  owner keeps engines, the scheduler keeps automations, the memory manager
  keeps trimming.
* **`GET /api/governor`** — one composed read model (governor + sysinfo
  hardware + engine metrics + construction-time capabilities); the System
  Centre renders the Runtime Governor card.

## Current evidence (what backs the claims)

* Deterministic unit: governor policy matrix; synchronization contract;
  abort-marker folding; pause/resume family; edit-transaction fault
  windows; llama.cpp CLI contract against real `--help` fixtures;
  the v1.8.4 flush-scheduler wedge/self-heal matrix (13 tests);
  zero-session Send (5 store-level scenarios); context-refresh races
  (7 causally ordered scenarios); staged-identity window (3);
  GPU-posture repair boundaries (5); the streaming/zero-session/context
  store suites run the REAL store over a scripted transport.
* Race: governor suite; the CI race gate (api, agent, sessions,
  contextplan, histref, runtime). Abort-after-resume now passes 15/15
  stressed (was ~1-in-6 flaky at v1.7.6 baseline).
* Integration: API contract tests including `/api/governor`; restart
  recovery; cross-mode surfaces.
* Browser E2E: the full real-stack suite including the v1.8.2
  visible-before-completion proofs, the v1.8.4 suspended-rAF run
  (streams and settles with requestAnimationFrame dead), and the
  v1.8.4 zero-session suite (delete-all → Send → auto-create → live
  stream → reload persistence; Chat + Agent).
* CI: Linux headless gates green in the producing environment. The
  Windows pause/resume defect was repaired against the exact CI scenario
  deterministically — **no physical-PC Windows runtime claim is made.**
  The v1.8.4 streaming wedge repair is proven on healthy Chromium and by
  construction at the unit level; the reported WebView2 runtime needs
  the user's own physical acceptance pass before any physical claim.
* Known limitations: CPU policy inert on Windows (live load authority
  doesn't exist there yet — reported as unknown); GPU/NPU surfaces are
  detection-level; the AUTO Vulkan path is repaired and tested but a GPU
  execution claim still requires the transaction's own runtime offload
  evidence on real hardware; CI ≠ physical-host runtime proof.

## Durable engineering invariants (never weaken)

1. One authority per concern (run lifecycle, session store, tool registry,
   scheduler, downloader, installer/update path, engine lifecycle, memory,
   runtime policy).
2. No sleep-based correctness; deterministic tests synchronize on
   authoritative state/events; no "wait longer" polling.
3. Never weaken production behavior to satisfy a test; never delete or
   skip tests to hide failures; fault-injection panics are intentional.
4. Never let an erased/discarded draft enter final history; semantic
   continuation is the honest pause/resume model.
5. Hardware detection ≠ execution evidence; GPU/NPU claims stay separated;
   numeric/unsupported engine arguments fail before spawn; the modern
   llama.cpp CLI contract (b10642/b11205 split cache types, load-mode) is
   verified against fixtures — inspect the exact binary `--help` before
   changing compatibility logic.
6. Never kill processes as resource policy; protection is cooperative.
7. `LICENSE.md` is the ONE license artifact. Keep the codename gate
   enabled and exact. Keep release identity version-only (no codenames).

## Regression-sensitive surfaces (re-check after ANY change)

* Startup/maintenance gate ordering (gate before engine/model prewarm);
  two-boot idempotency; identity fallback/corruption matrix; the v1.8.4
  staged-identity marker lifecycle (written at swap, cleared at
  commit/rollback; boot identity + caps probing key off it while present).
* Pause/Edit/Resume: durable checkpoint; revision consistency; edit
  journaling; restart recovery; empty-draft semantics; pause-after-resume;
  stop/abort-after-resume; one authoritative run.
* WebSocket run snapshot/replay and sequence continuity.
* Sessions: stale refresh AND stale startup responses cannot resurrect
  deleted state or drop created state; the delete-vs-in-flight-GET
  interleavings are pinned deterministically
  (`src/session-delete-regression.test.ts` — the store runs under
  `node --test` via `src/extensionless-ts-resolver.mjs`);
  pending-session delete cleans every backend authority
  (`internal/sessions/delete_pending_test.go`);
  the zero-session state stays valid and Send creates + activates
  (`src/zero-session-send.test.ts`, `e2e/zero-session.spec.ts`).
* Streaming flush: the three boundaries + recoverable controller
  (`src/stream-flush-scheduler.test.ts`); a lost MessageChannel delivery
  must NEVER wedge the latch; the activity flush must never be
  rAF-dependent again; cumulative-replace and no-double-processing
  contracts are unchanged.
* v1.8.5 hub delivery: the conflation queue
  (`internal/api/hub_conflation_v185_test.go`) — a full, never-drained
  queue must end on the NEWEST snapshot; terminal events must survive a
  full conflatable queue; publish must never block; close drains before
  reporting closure.
* v1.8.5 reasoning ladder: `internal/agent/reasoning_budget_v185_test.go`
  + `src/reasoning-controls.test.ts` — the wire field name
  (`reasoning_budget_tokens`), the exact budgets (0/1024/4096/nil),
  local-vs-remote gating, legacy migration, and the payload/visibility
  split (showThinking NEVER enters the payload).
* v1.8.5 execution ladder: `internal/llm/execution_v185_test.go` —
  detection never equals verified; the stage is monotone; the JSON wire
  contract is pinned.
* Context surface: only the newest context response may write
  `sessionContext`; deletion/creation/mode/run transitions invalidate
  (`src/context-refresh-race.test.ts`).
* GPU posture: explicit user OFF (`gpuAutoOffloadUserSet`), manual layer
  counts and the CPU profile are never repaired; the recommendation never
  writes a derived OFF (`internal/config/gpu_posture_v184_test.go`,
  `internal/recommendation`).
* Config writes: unique per-write temp files; never reintroduce a shared
  fixed temp name (cross-writer rename races became hard engine-start
  failures).
* Tools: deep-copied metadata under race; generation-aware spec cache.
* Engine lifecycle: no orphaned processes; error normalization.
* Governor: admission stays conservative on unknowns; envelope and
  monitor's protection never disagree about what a level means.

## Next strategic direction

`ROADMAP.md` owns it, now phased: v1.8.6 (CURRENT) delivered PHASE 2's
execution-truth / resource-integration core (the full
`GPU detected ≠ … ≠ verified` invariant at the accelerator authority
with the structured `ExecutionReceipt`; the authoritative GPU
candidate transaction with premature launcher activation removed;
`/api/engine` + `/api/perf` on one accessor; the Governor's inference
footprint + measured engine RSS + the resource-aware run gate; Windows
CPU telemetry through the existing seam). **The REMAINING Phase 2 work
is next: the unified C++ execution path behind the ladder (native is
CPU-only, capabilities explicit), measured prefill/cache/TTFT
performance foundations (measure first), and a second GPU backend only
when the repository can provision, launch, test and verify it without a
second provisioning/selection system.** PHASE 3+ (v1.9 AI System
Builder, v1.10 Universal Agent, v1.11 Learning & Evaluation, v1.12
Advanced Model Intelligence, v2.0 platform end state) stays FUTURE —
NOT IMPLEMENTED, and must never be marked complete merely because it is
documented.

### Phase 2 entry notes (start here)

* The execution contract to consume: `internal/llm/execution.go`
  (`ComposeExecutionReport` + the stage ladder) and its `/api/engine`
  surface; selection decisions key off the VERIFIED rung, never
  detection posture. v1.8.6 NOTE: the accelerator's
  `ExecutionVerified` now means exactly that — enumeration selects
  (pending plan, CPU safety net) and the structured
  `accelerator.ExecutionReceipt` (`ValidFor(engineTag, variant)`) is
  the only persisted cross-boot verification evidence.
* The engine identity transaction (stage → exact executable → probe →
  launch → health → generation → evidence → commit/rollback) is the
  existing authority for any GPU proof — never verify one binary and
  serve another. v1.8.6 NOTE: the transaction's candidate proving mode
  (`gpuCandidateProving` inside `updateEngineVariantTx`) is the ONLY
  launch context where enumeration may enable offload — the verify hook
  still requires the measured offload line before commit; a normal
  serving launch enables AUTO offload only on proven execution, and the
  offload evidence is PER-BOOT (launchArgs resets it).
* The launcher seam for the truth model: `LlamaServer.autoGPUOffload`
  (serving = proven execution only; proving = selection evidence;
  CPU-forced enforced; manual `numGpu` verbatim). The resolution memo
  the engine surface consumes is signature-validated
  (`currentAcceleratorResolution`) — extend the signature, never bypass
  it.
* Reasoning budgets are per-request on the llama.cpp path
  (`reasoning_budget_tokens`, both managed builds verified); the NATIVE
  engine has no budget control yet — wiring real native thinking budgets
  is remaining Phase 2 native-engine work, never a prompt-side fake.
* The Governor stays the ONE policy authority; RAM stays memory
  capacity; no second sampler, no second policy engine, no fake
  VRAM/offload numbers; unknown measurements stay unknown. v1.8.6
  NOTE: the Governor folds the inference footprint
  (`SetInferenceSource`) and the run gate consults
  `GovernorAdmitsModelLoad` before every engine start — wire new
  resource facts through those seams, never around them. The Windows
  CPU seam is `governor/cpu_windows.go` over the shared
  `cpu_delta.go` state machine.
* The v1.8.5 conflation queue is verified — do not redesign it absent a
  measured regression.
