# changelog.md — SHEYTAN-Local-Agent Release History

**This file is the ONLY canonical release-history artifact.** The README
describes the current product only; per-release evidence lives in the
release tags, the tests that shipped with them, and `UPDATE.md` (current
maintenance behavior).

Every release was shipped with the same invariants: one authority per
concern, evidence over confidence, no sleep-based correctness, honest
hardware claims, the codename gate enabled.

---

## v1.8.7 — 2026-10-05 — deterministic Tier-2 discovery + the data-analysis authority upgrade

Focus: repair the v1.8.6 Windows CI failure in `internal/engdiscovery`
(Actions run 37273354268) at its root, then upgrade the EXISTING
`dataAnalysis` tool into the application's deterministic data-analysis
authority — computation moves out of the model and into local
execution, and the model receives compact results instead of raw
datasets. One data tool; no second manager; no speculative dependency.

1. **P0 — deterministic Tier-2 discovery (the Windows CI root cause).**
   The v1.8.6 frontier ordered QUEUED directories correctly, but
   multiple workers executed jobs concurrently and `emit()` sealed the
   whole scan on first ARRIVAL of the `MaxCandidates`-th candidate.
   Candidate discovery order was therefore race-dependent: an
   already-running deeper worker could report before a shallower worker
   and seal the scan
   (`TestFullScanPrefersShallowOverDeepLexicallyEarlierCandidate`:
   got `aaa-early\deep\llama-server.exe`, want
   `zzz-late-engine\llama-server.exe`). v1.8.7 keeps the bounded worker
   pool and the priority frontier and adds a DETERMINISTIC RETENTION
   BARRIER: workers are only ever handed jobs of the current (lowest
   outstanding) priority value — parallelism stays within one discovery
   level; candidates are buffered per level and retained in
   (level, path) order at the level drain; the scan seals at the drain
   of the level that satisfied `MaxCandidates`, never on first arrival.
   Retention is a pure function of the scanned dataset: parallel scan +
   same dataset + repeated executions → same winning candidate, for any
   worker count and any interleaving. `Workers` stays ≥ 1 configurable
   (not reduced to force determinism); no sleeps, no retries, no timing
   assumptions; timeout, cancellation, `MaxDepth`, noise skipping,
   deduplication and bounded memory preserved. Coverage:
   `internal/engdiscovery/discovery_barrier_test.go` (deterministic
   winner across worker counts 1–16, same-depth lexical determinism,
   multi-root class priority, sealed-scan leak/deadlock guard) plus the
   strengthened existing suite.

2. **P0 — repeated-scan priority inversion in the discovery cache
   removed.** The cache-parent seeding path enqueued every previously
   observed candidate's parent directory at class 1 BEFORE the scan
   started, pre-marking those directories as visited and DEMOTING any
   location the user tree would have discovered at its natural (higher)
   priority — a repeated scan could rank a previously-cached DEEP
   directory above a fresh SHALLOW one, and the retained winner could
   alternate between runs. v1.8.7 DEFERS cache-parent seeds until the
   barrier enters the class-1 band (every class-0 level fully drained),
   preserving the v1.4.0 intent (cached locations still rank ahead of
   broad volume recursion) without the inversion.
   `mergeCacheAll` now persists the cache in deterministic (tier, path)
   order so the next scan's same-level tiebreak is a pure function of
   the cache contents.

3. **P1 — avoidable Tier-2 scan work reduced (the 126 s Windows
   package runtime).** `noiseDirs` now also skips language-toolchain
   caches (`.cargo`, `.rustup`, `.go`, `.nuget`, `.dotnet`, `.gradle`,
   `.m2`, `.android`, `hostedtoolcache`, …), per-run temp churn
   (`temp`, `tmp`) and Windows update remnants — deep, high-latency
   trees that change every run and can never host a user engine
   install. Real engine hosts (`.local`, `bin`, scoop shims, `.ollama`)
   remain scanned; Tier 1 still covers PATH. Once the candidate budget
   is satisfied, child directories are no longer enqueued (they could
   never be visited — the scan seals at the current level's drain).

4. **P1 — the data-analysis authority upgrade (ONE tool: `dataAnalysis`).**
   Five new deterministic actions, all in-process pure Go on the
   existing dataset model and parse-once numeric cache:
   - `analyze` — ONE call for a complete compact dataset analysis:
     schema, missingness, numeric summary, categorical top values,
     distinct counts, strong correlations (|r| ≥ 0.6), IQR outlier
     summary, optional group-by and sample, and deterministic key
     findings. Configurable `sections` (default:
     `schema,missing,stats,categorical,outliers,findings`).
   - `aggregate` — multiple aggregations in ONE call
     (count/sum/mean/min/max/median/std/quantile with `q`) over one or
     more grouping columns (`by`/`byList`), with a `"<agg>:<column>"`
     spec grammar and a bare-agg × `columns` shorthand; groups sorted
     ascending by key.
   - `join` — deterministic local joins between two datasets
     (inner/left/right/full), explicit keys required (single or
     composite `leftKeys`/`rightKeys`), SQL-standard semantics with
     file-order determinism, a 2,000,000-row materialization cap, and
     bounded preview + optional artifact file.
   - `quality` — compact machine-readable diagnostics: missing values,
     duplicate rows, invalid numerics (±Inf in numeric columns),
     constant columns, high-cardinality columns, mixed numeric/text
     type inconsistencies and per-column outlier counts.
   - `export` — materialize rows (optionally filtered, projected,
     sorted, limited) as CSV/TSV/JSON artifacts under the workspace
     path authority; the model gets a path + counts, not pasted rows.
   All five stamp compact provenance metadata (backend, bytes, rows,
   cols) and honor three output modes: `compact` (default),
   `table` and `json`. Analysis results now answer most data questions
   from one deterministic tool call.

5. **P1 — JSON loading determinism repaired.** `loadJSON` derived its
   column order from Go map iteration, which is randomized — the same
   JSON file could produce a different column order (and therefore a
   different analysis result) on every load. Objects are now decoded
   with `json.Decoder` token streaming so column order follows the
   DOCUMENT's first-seen key order, deterministically, for both JSON
   arrays and JSONL. Pinned by `TestJSONColumnOrderIsDeterministic`.

6. **P1 — data-tool test surface established.** The package previously
   had no dedicated suite; `internal/tools/data_tool_test.go` adds
   focused coverage: CSV/TSV/JSON loading, type inference, missing
   values, the parse-once numeric cache, all five new actions,
   compact/table/json output modes, output limits, result
   materialization, cancellation, malformed datasets, path
   restrictions (traversal and absolute escapes rejected through the
   path authority), the honest 256 MB size bound and byte-for-byte
   deterministic repeated results. Orchestrator integration:
   `internal/agent/datatool_orchestrator_test.go` proves the real
   registration path; `internal/toolsets/datatool_selection_test.go`
   pins that `dataAnalysis` stays selected for data tasks.

Backend decision (documented, not hidden): the heavy-data backend
evaluation kept the pure-Go in-process engine. DuckDB's Go client is
cgo with a statically linked bundled engine — unacceptable packaging
and CI complexity (a C toolchain on the Windows/Linux packaging paths
plus tens of megabytes per target binary) for workload sizes the
256 MB fast path already covers. Parquet and SQL are therefore NOT
added; `query`/`aggregate`/`join` remain the relational authority, and
the 256 MB input bound is stated honestly (no streaming claim). The
remaining native C++ GPU work is unchanged and still not complete.

---

## v1.8.6 — 2026-10-05 — Phase 2: execution truth, GPU transaction authority, resource integration

Focus: PHASE 2's deep runtime/execution work — make the v1.8.5
execution/evidence foundation actually CONTROL backend selection and
resource behavior, prove GPU execution correctly, and integrate
CPU/GPU/RAM truth — through the EXISTING authorities (no second runtime,
no second selection authority, no second sampler, no second policy
engine). Five verticals landed, each pinned by deterministic suites:

1. **P0 — the execution-truth contradiction closed (enumeration is
   selection evidence, never execution proof).** The v1.8.5 ladder was
   correct conceptually but consumed an accelerator contract that could
   mark a GPU `ExecutionVerified=true` from `--list-devices` enumeration
   alone. v1.8.6 enforces the full invariant
   `GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
   verified` at the ONE accelerator authority
   (`internal/accelerator/resolveGPU`): enumeration now SELECTS
   GPU_VULKAN with the pending-execution verification plan and the CPU
   safety net; verification requires the measured runtime offload line
   or a still-valid **ExecutionReceipt** — a new small structured
   object (`accelerator.ExecutionReceipt`) carrying the identity the
   evidence was produced under (kind, line, engine tag, variant,
   device, model, status, time) with `ValidFor(engineTag, variant)`
   stale-evidence invalidation: a receipt from another engine build,
   another variant or a failed probe can NEVER verify the current
   selection. Coverage:
   `internal/accelerator/execution_truth_v186_test.go` (enumeration
   never verifies; offload line verifies; valid receipt verifies;
   stale/failed receipts never verify; CPU stays verified by
   definition; the receipt identity matrix) plus the corrected v1.2.6/
   v1.6.1 contracts.

2. **P0 — the GPU transaction is authoritative; premature activation
   removed.** The launcher (`LlamaServer.autoGPUOffload`) could turn on
   `--n-gpu-layers 99` from device enumeration (or even Vulkan DLL
   presence) BEFORE the bounded transaction proved real GPU execution.
   v1.8.6 splits the two modes: a NORMAL serving launch enables
   auto-offload ONLY on PROVEN execution (the current boot's measured
   offload line, or a persisted verified GPU-probe receipt whose engine
   tag+variant still match — `gpuExecutionProven`); enumeration/DLL
   presence keep AUTO CPU-safe. A CANDIDATE verification transaction
   (`gpuCandidateProving`, set strictly inside `updateEngineVariantTx`
   when the execution-evidence verify hook runs, transaction-scoped via
   deferred reset) may boot Vulkan from selection evidence to PROVE it
   — the transaction still requires a real generation AND the measured
   offload line before commit, so enumeration alone can never produce a
   verified serving posture. Manual `numGpu` config is applied verbatim
   and the CPU-forced profile is now enforced at the launcher too. The
   offload evidence became PER-BOOT (`launchArgs` resets it): a restart
   or model swap can never inherit the previous boot's GPU execution
   claim. Coverage: `internal/llm/gpu_activation_v186_test.go` (10
   tests: enumeration/DLL/failed-receipt never activate; offload line/
   valid receipt do; CPU-forced stays CPU; manual config respected;
   proving mode; per-boot reset) plus the existing v1.7.2 rollback
   suites unchanged.

3. **P0 — `/api/engine` and `/api/perf` agree (one authority, one
   evidence path).** The engine execution ladder previously depended on
   a memo written only by the `/api/perf` poll — the authoritative
   engine state accidentally depended on whether the performance page
   happened to poll first. v1.8.6: ONE accessor
   (`currentAcceleratorResolution`) serves BOTH surfaces; the memo now
   records its input SIGNATURE (engine tag + variant + requested
   profile + loaded model + offload evidence + probe state) and a stale
   memo is recomputed before any consumer can read it — no stale
   poll-only snapshot can claim a current backend; the engine surface
   refreshes the memo itself (a background warm-up at server start
   keeps the first poll off the enumeration cost); model/engine/device
   identity stays aligned, and the stage cannot move backwards because
   a UI poll happened later (only because serving reality changed).
   Coverage: `internal/api/engine_perf_consistency_v186_test.go`
   (engine-first polling, verdict agreement both orders, monotone
   stage across polls, stale-memo invalidation, fresh-memo reuse).

4. **P0 — real CPU/GPU/RAM resource integration through the ONE
   Governor.** The Governor now accounts for the CURRENT inference
   workload: a new injected footprint source (wired on the Stack from
   the EXISTING model-card/context authorities — model file bytes as a
   FILE fact, planned KV at the serving window) folds into the resource
   state and the envelope (a footprint that consumes the resident
   budget reduces background work through the existing honest
   adjustment class, reason stated; unknown stays unknown — RAM is
   memory, never an accelerator). The v1.8.6 RESOURCE-AWARE RUN GATE
   (`Stack.GovernorAdmitsModelLoad`, consulted by EnsureLLM and
   EnsureLLMContext BEFORE any engine start) admits the model load
   against the Governor's measured envelope using the SAME resident
   plan the preflight authority computes (weights + KV + runtime
   overhead); sustained pressure defers the load with the explainable
   reason; an unmeasured Governor falls through to the preflight gate
   exactly as before. The measured engine RSS now actually flows:
   LlamaBackend.Metrics reads the engine process resident set through
   the existing `resources.ProcRAM` authority (the same sampler family
   the live monitor uses), feeding the Governor's engine facts. A new
   `/api/perf` `engineMemory` block carries the serving engine's REAL
   memory evidence with provenance labels — measured process RSS (from
   the one monitor cadence), the model FILE size (explicitly never
   "RAM used"), the runtime offload line, and unknown KV allocation
   named as unknown (never a guessed figure). Coverage:
   `internal/governor/inference_footprint_v186_test.go` (6),
   `internal/runtime/governor_gate_v186_test.go` (6: wiring, unknown
   fallbacks, healthy admission, sustained-pressure deferral).

5. **P1 — Windows CPU telemetry through the EXISTING seam; native and
   GPU-backend truth preserved.** The Governor's Windows CPU seam
   (`governor.CPULoadPlatform`) now measures real load via kernel32
   `GetSystemTimes` through the ONE shared priming/delta state machine
   (`internal/governor/cpu_delta.go`): the first sample primes,
   subsequent samples compute the real busy-fraction delta, failures
   stay unknown (the cumulative-counter baseline is kept so the next
   delta stays true), and the Linux seam is untouched — no second
   sampler, no second cadence, the Governor's poll-path ownership
   unchanged. Coverage: `internal/governor/cpu_delta_test.go` (5,
   cross-platform, race-gated). The native C++ engine keeps Go as the
   control plane with its capabilities explicit (CPU-only execution,
   no GPU claim, no faked reasoning budgets — now stated in the
   engineMemory surface); the Intel/GPU direction stays the PROVEN
   llama.cpp Vulkan transaction (no SYCL/OpenVINO backend was added —
   the repository can not yet provision, launch, test and verify one,
   so none is claimed).

Phase 1 surfaces are preserved unchanged: live streaming conflation
queue, server conflation, the reasoning ladder, Show/Hide Thinking,
zero-session Send, generation guards, pause/edit/resume, abort
honesty, engine identity transaction, config atomicity, Governor
ownership, reconnect/replay and the one-authority architecture
(full llm/api/runtime/governor/accelerator suites + the race gates
green; 213 frontend unit tests and the release-metadata gate green).

---

## v1.8.5 — 2026-10-03 — Phase 1 of the staged engine program

Focus: PHASE 1 — the core runtime / user-surface foundation of the staged
engine program (the plan: Phase 1 runtime/user foundation → Phase 2 deep
execution-engine/resource integration → Phase 3+ new product capabilities).
Four user-facing/engineering verticals landed, each through the EXISTING
authorities (no new manager, scheduler, registry, downloader or runtime):

1. **P0 — live streaming: the server-side drop-newest bottleneck closed.**
   Trace (engine → SSE client → orchestrator emitProgress ~8ms → publish →
   hub → WS → frontend fast-path fold → triple-boundary flush → render)
   showed the v1.8.4 frontend scheduler robust; the remaining defect was
   the run hub's subscriber channel: on overflow it DROPPED THE NEWEST
   event ("a slow WebSocket must never block the agent run"). For
   cumulative response/reasoning captions that discards the frame with
   the FULL text while the buffer keeps stale prefixes — under transport
   backpressure the visible text freezes at a stale prefix until the run
   ends (Stop included), then the reconnect snapshot replay reveals
   everything at once: the server-side twin of the v1.8.4 MessageChannel
   wedge. v1.8.5: bounded conflation-aware subscriber queues (overflow
   evicts the OLDEST conflatable snapshot — newest-wins; order and the
   seq replay contract preserved; terminal events never preferentially
   evicted; publisher never blocks; memory bounded) plus a generous
   per-event write deadline (a wedged client tears down deterministically
   and recovers via reconnect replay). Coverage: 9 deterministic
   hub/queue suites including the drop-newest reproduction, terminal
   survival, publish-never-blocks (20k offers to a wedged reader),
   close-drains-first, concurrent publishers (race gate); the full API
   suite green.

2. **P0 — reasoning depth becomes a REAL four-level ladder: Low / Mid /
   High / Ultra.** Every level carries a numeric thinking-token budget
   applied to the generation request through the llama.cpp request-level
   `reasoning_budget_tokens` parameter — VERIFIED against the actual
   server sources of BOTH managed builds (b10642 DefaultEngineTag and
   b11205; the field exists at the OAI chat endpoint and maps into the
   reasoning budget sampler; the CLI fixtures independently document
   `--reasoning-budget`): low=0 (thinking off — "0 for immediate end"),
   mid=1024 (bounded default), high=4096, ultra=-1 → NOT SENT (engine
   default/unrestricted — the honest encoding of "no client-side cap").
   Pointer-typed field keeps the explicit 0 on the wire; local engines
   only (same gating as TopK/NumCtx/MinP); non-thinking models ignore the
   budget (no fabricated reasoning); the native C++ path is
   documented-inert. The level rides EVERY run request; legacy
   auto/fast/thinking values migrate at the wire and persistence
   boundaries. Tier posture: low keeps the latency-first floor, mid is
   neutral, high/ultra never inflate context (depth = budget, not
   context). Coverage: 9 Go suites (normalization, budgets, local/remote
   gating, ultra-nil, legacy migration, wire field name, defensive) +
   the tier-posture pin + 4 store-level scripted-transport suites
   (payload always carries the level; visibility split).

3. **P0 — Show Thinking / Hide Thinking (visibility only).** A persisted
   presentation preference gating the rendering of backend-reported
   reasoning on the live bubble, the safety-net bubble and history
   messages. It never touches the request payload (pinned), never changes
   the reasoning level or budget, keeps the store folding reasoning
   snapshots, and displays only what the backend actually reported —
   unavailable reasoning stays unavailable. Shared Chat/Agent semantics
   through the shared surfaces.

4. **P1 — the engine leaves the user's critical path.** The Start/Stop
   engine toggle is removed from the ordinary workflow; the engine boots
   on first use (run gate), restarts after engine-affecting changes
   (settings flow) and recovers through internal supervision; the UI
   represents engine state. Internal lifecycle operations (startup,
   update, restart, recovery, shutdown) unchanged — supervision
   ownership untouched.

5. **Phase 2 foundation — the ONE execution/evidence ladder.**
   `internal/llm/execution.go` + `/api/engine`'s `execution` block:
   detected → backend-available → device-selected → model-loaded →
   generation-executed → execution-evidence → verified, a PURE composer
   over the existing authorities (device presence, backend health, the
   accelerator selection memo, verified model, measured generation ring,
   runtime offload lines). Monotone by construction: device enumeration
   can never equal verified execution; unknowns stay unknown; gaps are
   named. Coverage: 5 deterministic ladder suites incl. the cardinal
   rule and the wire contract.

6. **Version identity** — all release surfaces at 1.8.5 through the ONE
   canonical gate (`node scripts/release-version.mjs --check`).

Not claimed by this release: physical GPU execution proof, the unified
C++ execution path, native reasoning budgets, Windows CPU telemetry —
all explicitly Phase 2 scope (`ROADMAP.md` §Phase 2). CI remains CI, not
physical-host runtime evidence.

---

## v1.8.4 — 2026-10-01

Focus: four P0 defect families — (A) live streaming visibility, (B) the
AUTO Vulkan posture circularity and the engine-identity verification
window, (C) the zero-session Send dead end, (D) stale context-usage
races — plus the reliability defect the verification funnel surfaced
(the fixed `config.json.tmp` name racing concurrent config writers). No
new authority was created anywhere: every fix routes through the
existing streaming scheduler, the existing variant transaction, the
existing session store and the existing context backend.

1. **P0-A — the streaming flush can no longer wedge permanently, and the
   activity timeline is no longer rAF-only.** Root cause of the real
   Windows runtime report ("live response/thinking invisible during
   generation, appears only after Stop"): the v1.8.2
   `MessageChannelTaskController` was ONE-SHOT — its port handler nulled
   the channel after the first delivery and a post arriving while a
   message was still in flight chained onto the armed callback WITHOUT
   posting a new message. A single lost or indefinitely delayed
   MessageChannel message therefore left the controller wedged forever
   (queued armed, channel non-null, microtask fallback unreachable, the
   scheduler latch stuck pending) — every later `schedule()` became a
   no-op and streamed text accumulated in the accumulator until the Stop
   path's synchronous flush revealed it, exactly matching the observed
   runtime (the phase label kept updating because the fast path's
   `transitionPhase` renders synchronously at socket-receive time).
   v1.8.4: the flush scheduler arms THREE independent boundaries behind
   the ONE coalescing latch — a reusable MessageChannel macrotask, a 0ms
   timeout task (the primitive class proven alive in the failing
   runtime), and the animation frame; the task controller is
   latest-callback-wins with an explicit in-flight handshake and a
   deterministic microtask fallback the moment a post arrives while a
   message is still undelivered (a lost channel degrades to microtask
   delivery instead of dying). The ACTIVITY flush — statuses, tool
   events and the done/error/aborted lifecycle events — moved off its
   rAF-only schedule onto the same triple-boundary scheduler. Evidence:
   `src/stream-flush-scheduler.test.ts` (13 tests, including the
   lost-message wedge reproduced deterministically — the v1.8.2 design
   fails it, the v1.8.4 design self-heals), the store-level suites, and
   a new browser E2E proving live text and honest run settlement with
   requestAnimationFrame fully suspended.

2. **P0-B — the AUTO Vulkan candidate is no longer blocked by a stale
   derived posture, and the verification window reports the binary that
   is actually on disk.** Two root causes:
   (i) The pre-1.8.4 recommendation pipeline wrote `gpuAutoOffload=false`
   + `numGpu=0` as a DERIVED posture ("CPU-only until a Vulkan engine
   build is provisioned") on every auto model selection; the AUTO probe
   eligibility gate then read that posture as an explicit OFF and refused
   the candidate forever — the exact circularity in the real runtime log
   ("GPU AUTO Vulkan candidate not considered this boot: GPU offload is
   disabled in settings (numGPU=0, auto-offload off)"). The
   recommendation never writes a derived OFF anymore (the launch-time
   evidence gate `autoGPUOffload` keeps the engine honestly on CPU while
   no usable device exists), a new `gpuAutoOffloadUserSet` config field
   marks EXPLICIT user actions (settings toggle, direct config patch),
   and `config.Load` repairs the legacy derived state ONCE — honestly
   noted and persisted — while never touching an explicit user OFF, an
   explicit manual layer count, or a CPU requested profile.
   (ii) The real log sequence "b11310 staged + SHA verified → engine
   boot probe: binary build b11273 → b11310 committed" was an identity
   violation in the deferred-commit window: the boot path keyed its
   identity on the RECORDED tag (installed.json), which still described
   the previous build until Commit ran, so the log misreported the
   serving binary AND the old build's persisted capability profile
   shadowed the new binary. The installer now writes a window-scoped
   staged-identity marker (`engine-stage-pending.json`) the moment the
   byte-verified candidate is swapped in; the boot probe probes, caches
   and reports the ACTUAL staged build; Commit and Rollback clear the
   marker. Evidence: `internal/updater/staged_identity_v184_test.go`
   (marker present during the window with the old recorded tag,
   cleared on commit and rollback, corrupt marker fails closed),
   `internal/config/gpu_posture_v184_test.go` (repair + explicit-OFF +
   manual-layers + CPU-profile boundaries, persisted once),
   `internal/recommendation` contract updates.

3. **P0-C — pressing Send with zero sessions creates and activates a
   session and continues the run.** Root cause: `run()` already created
   the session lazily, but the composer textarea and Send button were
   hard-disabled on `!activeSessionId` — with zero sessions the lazy
   creation was unreachable (the reported "Send/chat is blocked instead
   of creating a session"). The composer now enables on a zero-session
   space (the model gate and the live-run gate keep their semantics),
   the placeholder and footer state the truth ("sending starts a new
   session"), and `deleteSession` already left a valid zero-session
   state. Evidence: `src/zero-session-send.test.ts` (store-level:
   create + activate + continue the same send; valid zero state after
   deletion; the exact delete-then-send user path; honest rejection when
   creation fails; the created session survives a refresh),
   `e2e/zero-session.spec.ts` (real-stack: delete every session →
   zero-session state → Send → session created + active → user bubble →
   streamed text visible BEFORE completion → reload persistence; Chat
   and Agent modes).

4. **P0-D — stale context responses can never overwrite newer context
   state.** Root cause: `refreshSessionContext` guarded only on the
   active session id, so two concurrent requests for the SAME session
   could resolve out of order (older lands last), and deleting the
   active session never cleared or refreshed the visible context.
   v1.8.4 adds a monotonic request generation: every refresh claims a
   strictly newer generation; a response may land only when it is still
   the newest request AND its session is still active. Session switch,
   session deletion (with replacement refresh), session creation, mode
   switch and a fresh run's state transitions all invalidate the
   generation; the UX stays automatic/unlimited (no user-controlled
   context-size controls; physical limits remain backend-governed).
   Evidence: `src/context-refresh-race.test.ts` (7 scenarios over a
   scripted transport with causally forced response ordering: same-
   session out-of-order, triple-race, cross-session held response,
   deletion clears, deletion refreshes a replacement, creation clears,
   pre-run response invalidated).

5. **P1 — `config.Save` atomicity hardening (surfaced by the funnel).**
   The fixed `config.json.tmp` name let two concurrent config writers
   rename each other's temp file away — observed in this repo's own E2E
   run as a hard engine-start failure ("persist default engine path:
   rename …config.json.tmp …: no such file or directory"). Every
   config write now uses a unique same-directory temp file (CreateTemp +
   rename), preserving the atomic-rename semantics without the name
   collision. `config.Save`, the v1.8.4 posture repair and the sampling
   repair all share the helper.

Version surfaces are 1.8.4 (package.json → `release-version.mjs` →
config.go / build/config.yml / SIGNATURE); the codename gate and the
release contract tests pass unchanged.

---

## v1.8.3 — 2026-10-01

Focus: root-cause and repair the exact Linux CI session-delete failure
(Actions run 36713108772, job 109880449134: Browser E2E 30/31,
`e2e/sessions.spec.ts:88` "delete session removes it and activates a
remaining one" — `Expected: < 4, Received: 4` after 15 s), and close the
real defects the investigation exposed. No new authority was created; the
v1.8.2 streaming, memory-evidence and self-model behavior is untouched
(the full browser suite, including every live-stream test, passes).

1. **P0 — the run-107 failure root-caused with instrumented evidence.**
   An instrumented diagnostic (request/response journaling + server-side
   authoritative list snapshots + DOM identity capture, with the browser's
   create response delivery delayed to reproduce CI latency) proved the
   interleaving: the test sampled its count baseline BEFORE the
   asynchronous "New session" create landed in the sidebar (its
   `expect(first()).toBeVisible()` gate was satisfied by the PRE-EXISTING
   items, not the new one); the delete then correctly removed exactly one
   session — verified absent from the server's authoritative post-delete
   list — while the just-created session's late response kept the sidebar
   count flat. The product deleted the correct session and never
   resurrected it; the count arithmetic compared equal numbers for 15 s.
   A passing run of the same test was the same interleaving with the poll
   catching the transient count window — a coin flip on runner latency.

2. **P0 — the browser test repaired and strengthened (measurement
   synchronizes on state, never on timing).** The repaired test waits —
   state-based, no sleeps — for the created session to actually appear
   (the count grows by one) before sampling its baseline, then asserts
   the SPECIFIC deleted id (new `data-session-id` rows; the rendered
   8-char id slice is ambiguous within the same bucket), the survivor
   identity set, the count, replacement activation, composer usability,
   and — after a full reload — that the deleted id is still absent from
   both the sidebar and a fresh authoritative list fetch. No timeout was
   increased; nothing was weakened.

3. **P0 — the startup session-list write now goes through the ONE
   generation guard (the v1.7.5 authority).** `initializeAgentOnce`
   previously applied its `GET /api/sessions` response WITHOUT a ticket —
   the v1.7.4 stale-response defect class survived in the init consumer.
   The sidebar's "New session" button is actionable while the startup GET
   is on the wire, so a user create landing in that window was clobbered
   by the stale startup list: the created session VANISHED from the
   sidebar. The repair: the init takes a `sessionListGuard` ticket before
   the GET; a response may only land while its ticket is current and the
   mode unchanged; a superseded response applies only the non-session
   state and delegates the list + selection re-resolution to
   `refreshSessions()` (its own ticket); the eager first-install create
   invalidates the guard exactly like `store.createSession` does. Verified
   by mutation at two layers: the store-level suite and a real-browser
   E2E both FAIL against the unguarded v1.8.2 init and PASS with the
   repair.

4. **P0 — deterministic session-delete coverage at three layers.**
   `src/session-delete-regression.test.ts` (11 scenarios against the REAL
   store over a scripted HTTP transport where response ordering is forced
   by causality — pending/persisted delete, replacement selection,
   delete-vs-held-GET, stale-GET non-resurrection, mode separation,
   honest duplicate-delete, post-delete fresh fetch, init-race survival,
   clean-init apply, eager first-install session preserved — plus a
   Node resolve hook making the store testable outside the bundler);
   `internal/sessions/delete_pending_test.go` (pending delete removes
   every authority; persisted delete cleans index/file/sidecars with a
   fresh-store no-resurrection check; unknown-id honest failure); and the
   new browser E2E for the init race.

5. **P0 — a failed DELETE is never silently swallowed.** The sidebar
   invoked `void deleteSession(id)`; a server-side DELETE failure was an
   invisible unhandled rejection and the session stayed in the list with
   no explanation. `deleteSession` now surfaces the failure through the
   same error state `renameSession` uses (state untouched on failure,
   never a fake success).

6. **P0 — the created-session prepend is idempotent.** A list refresh
   whose response lands between the create POST's dispatch and its
   arrival can already contain the created session; the unconditional
   prepend then produced a duplicate row. The prepend now dedupes by id —
   the new session is always the first row and appears exactly once.

7. **P1 — roadmap repaired.** `ROADMAP.md` now states the artifact truth
   (changelog = sole release history; README current-only; UPDATE.md =
   current release/maintenance evidence) and carries the evidence-ranked
   **Performance, Reliability & Scale Backlog** (D/I/H labels). External
   report claims — percentages, TPS/TTFT/cache/VRAM figures, "3x/5x/80%"
   numbers, Tokio/asyncio rewrites, mandatory FAISS/USearch/SQLite/
   bbolt/tree-sitter, NPU embedding services, dual-model VRAM swapping —
   are explicitly treated as hypotheses or rejected; the one-authority
   architecture and the Go runtime are preserved.

Verification: frontend typecheck/lint/unit 191/191; `go test ./internal/...
-tags headless` 58 packages; race suite (api/agent/sessions/contextplan/
histref/runtime); native C++ build + ctest 12/12; full browser E2E
32/32 (the repaired delete test and the new init-race test included);
stress suite 47/47 with zero hangs/crashes; release metadata and codename
gates in check mode. Windows acceptance on a physical machine remains
outstanding until observed (CI is not physical-host evidence).

---

## v1.8.2 — 2026-09-30

Focus: make live generation genuinely visible and self-describing; remove
avoidable per-turn overhead; clean human-facing logs. No new memory or
capability authorities were created — every feature consumes the existing
ones.

1. **P0 — the "text visible only after Stop" live-rendering defect,
   root-caused and repaired.** v1.8.1's single-frame fast path folded
   stream-critical events into the accumulator at socket-receive time, but
   the FLUSH was armed exclusively on `requestAnimationFrame`. In the
   affected WebView2 runtimes the compositor's frame callbacks can be
   throttled or suspended while the JS event loop, the WebSocket and
   React's own scheduler keep running — phase labels updated, elapsed
   clock ticked, streamed text never appeared, and pressing Stop (whose
   path flushes synchronously) revealed everything at once. The flush is
   now scheduled on TWO deterministic boundaries — an event-loop task
   (MessageChannel, the primitive React's scheduler itself uses) AND an
   animation frame; whichever runs first flushes, the other no-ops.
   Exactly one coalescing latch remains (never one render per token), and
   visibility no longer requires frame callbacks at all.
   Evidence: `src/stream-flush-scheduler.test.ts` (8 deterministic tests:
   one-flush latch, first-boundary-wins, cancel, task-only sufficiency),
   plus the real-stack browser harness (`e2e/repro-stream.mjs`) tracing
   socket→fast-path→accumulator→flush→DOM.

2. **P0 — the live surface shows more of what is happening.** The live
   generation bubble now carries: (a) a concise FACTUAL state while the
   model emits no reasoning stream ("Thinking · this model is not
   exposing a reasoning stream") — never fabricated reasoning, never
   generated labels presented as model thoughts; (b) a backend-truth
   MEMORY indicator (see 3); (c) the existing phase, cursor, reasoning
   panel and runtime activity, unchanged in authority.

3. **P0 — memory boosting made real, visible and evidence-backed.** The
   orchestrator composes a `MemoryEvidence` record at the injection site
   from the survival-reconciled injection facts (rolling session summary
   injected + measured tokens; recalled exchanges actually carried; the
   attached cross-mode history references; whether recall was attempted
   at all) and publishes it on the existing `context` activity. The live
   UI renders exactly this record — e.g. `Memory: session summary · 2
   recalled exchanges` or `Memory: session summary · no recall matches` —
   and nothing when the report has not arrived. A block the history
   windower elided is never claimed. No second memory engine: the
   selection policy remains the existing tiers (FAST stays cheap; targeted
   recall fires on memory-relevant intent; the rolling summary stays
   automatic). Evidence: `TestContextActivityCarriesMemoryEvidence`,
   `TestMemoryEvidenceOnContextPlan` (Go),
   `src/memory-evidence.test.ts` (9 unit tests).

4. **P0 — the model can accurately describe its own tools and
   capabilities.** A deterministic capability-intent signal
   (`taskclassify.Signals.SelfDescribe`) fires on vocabulary like "what
   tools do you have?" / "what can you do?" / "what model are you
   running?"; on that intent the orchestrator injects ONE bounded runtime
   self-model block composed from the EXISTING authorities — the tool
   registry snapshot (`ShortDescription()` first, bounded first-sentence
   fallback), the already-resolved model card (architecture, quantization,
   parameters, context limit, tokenizer family, chat template, vision only
   when real, native-executability verdict), config-backed backend facts
   (provider, installed engine build, streaming/cancellation support,
   research availability, Lab and memory availability), the sysinfo fast
   snapshot (OS/CPU/RAM measured; GPU/NPU labeled detection-only) and the
   turn's memory plan. Truthful distinctions: registered vs enabled vs
   offered-this-request vs disabled-and-NOT-callable. Capability questions
   stay cheap: no research, no recall, no repo indexing, no extra engine
   turns. Evidence: `TestClassifySelfDescribeIntent`,
   `TestSelfDescribeAddsNoComplexity`, `TestCapabilityIntentInjectsSelfModel`,
   `TestOrdinaryChatDoesNotInjectSelfModel`, `TestBuildSelfModelCatalog`.

5. **P1 — human-facing logs lose the opaque identity tokens.** `runId=`,
   `runID=`, `session=`, `sessionId=`, `sessionID=` tokens are redacted at
   the ONE central sink (the log record formatter), so app.log, the UI
   LogViewer ring, stderr and crash-report text all inherit the rule.
   Timestamps, severity, subsystems, durations, outcomes, error causes and
   ordinary prose containing the word "session" survive; internal identity
   (API objects, run state, journals, storage keys) is untouched.
   Evidence: `internal/logging/redact_test.go` (18 cases + idempotence +
   diagnostic-field preservation).

6. **P1 — repeated per-turn model-card parsing eliminated.** The model
   capability cache replaced its arbitrary 10-second TTL with
   identity-based caching: the immutable parsed GGUF card is cached under
   (path, size, mtime) with no expiry, and the config-sensitive derived
   fields re-derive from the cached card whenever a configuration
   fingerprint changes — a turn separated by minutes no longer re-reads a
   32 MiB metadata block. The map is bounded; a replaced model file
   re-parses exactly once. No second capability authority.
   Evidence: `internal/llm/modelcaps_cache_v182_test.go`.

7. **P1 — README is current-only; release history moved to this file.**
   The README no longer carries release-history sections or duplicated
   chronology; `changelog.md` is the canonical history. `ARCHITECTURE.md`
   (architecture), `agent.md` (current handoff) and `UPDATE.md` (current
   operational notes) remain the other documentation authorities.

8. **P1 — engine identity log stages made explicit.** The boot-time
   capability probe now reads `engine boot probe: binary build <tag> …`
   and the deferred-commit line names the build that became active
   (`… build <tag> committed as the active engine (next boot probes it)`),
   so a log sequence `staged → boot probe → committed` is unambiguous
   about which build is serving and which is staged.

---

## v1.8.1 — 2026-09-30

* **P0 — real-Windows local-generation crash repaired at the root.** On a
  machine running a small local Gemma-class model, an ordinary `hi` turn
  panicked with a nil pointer dereference between `task classified` and
  `tier selected`, settling as an error in ~57 ms. Root cause (reproduced
  deterministically): Gemma-class GGUFs carry ~262K-entry tokenizer blocks
  whose metadata exceeded the model-card parser's 8 MiB read bound, so the
  capability card resolved to nil and the orchestrator's estimator block
  dereferenced it. Repairs: the documented nil-card fallback (configured
  context + conservative estimator) in `resolveEffectiveContext`, and a
  32 MiB read bound so real Gemma-class cards parse again.
  Evidence: `TestResolveEffectiveContextSurvivesUnreadableCard`,
  `TestGemmaClassCardParsesUnderRaisedBound`, `TestLocalGemmaHiChatCompletes`
  (the api package's first local-provider run-level E2E),
  `TestLocalChatSurvivesUnreadableModelCard`.
* **P0 — streamed answers became visible through ONE render frame instead
  of two.** `src/stream-fast-path.ts` folds stream-critical events into the
  streaming accumulator the moment the WebSocket delivers them; the
  timeline batch, sequence/stale-run protection, cumulative-snapshot
  semantics, replay idempotence and synchronous done/error/abort flushing
  are preserved (12 deterministic frame-controller tests). (v1.8.2 note:
  the flush-boundary dependency this left on `requestAnimationFrame` is
  repaired above.)
* **P1 — engine rollback restores the recorded identity** — a failed
  startup verification after a package swap could leave the transient
  bundled-default tag stamped in `installed.json`; `Rollback()` now
  re-records from the restored manifest.
  Evidence: `TestRollbackRestoresRecordedIdentityFromManifest`.
* Audited, no change: the AppData root migration contract; GPU/AUTO CPU
  with `numGPU=0` as the honest evidence-ladder outcome.

## v1.8.0 — 2026-09-30

* **P0 — pause→resume→pause synchronization repaired at the root** —
  after a resume, proof of live generation requires a strictly newer run
  sequence AND a changed cumulative snapshot (`resumedGenerationEvidence`).
* **P0 — honest abort marker end-to-end** — the orchestrator publishes
  `aborted` (never `done`) for canceled generations; the live state, the
  outcome registry and the frontend agree; the abort-after-resume flake
  is gone (15/15 stressed runs).
* **Runtime Governor vertical slice (Adaptive Runtime Intelligence)** —
  resource state with explicit unknowns, pressure model with rolling
  signals, execution envelope with honest adjustment classes, resident-
  budget admission, provenance-carrying self-model. Policy only.
* **`GET /api/governor` + System Centre Governor card** with real values
  and named unknowns.
* Documentation consolidated; version truth through the canonical gate.

## v1.7.0 → v1.7.6 (compact)

* **v1.7.6** — edit-transaction journals with ten-window crash/fault
  recovery matrix; restart-recovered runs claim the checkpoint revision;
  paused surface excludes journals; token-aware codename gate;
  generation-aware spec cache.
* **v1.7.5** — transactional pause/edit/resume (CAS validation, transcript
  replace, checkpoint commit, publication last); stale session-list
  generation guard; numeric-variant gates.
* **v1.7.4** — pause/edit/resume state machine, durable checkpoints, WS
  replay continuity, double-pause idempotency, stop-after-pause semantics.
* **v1.7.3** — recovery/handoff durability and lifecycle hardening.
* **v1.7.2** — scheduler settlement contract, live-protection hardening.
* **v1.7.1** — preflight gate (PREFLIGHT → REFUSE → NO ENGINE START), live
  monitor with hysteresis + cooperative critical protection, native engine
  first-class backend, context-exhaustion recovery.
* **v1.7.0** — reliability pass: engine lifecycle, hub replay, stress
  surfaces.

## v1.5 → v1.6 (compact)

Multi-agent context, memory/recall tiers, engine-variant provisioning
through the authoritative resolver, Vulkan asset validation, clone +
repository indexing, Lab workspaces, stable embedded-frontend asset
contract.

## v1.3 → v1.4 (compact)

Durable summaries, cross-mode history, task state, handoff, continuum
rollover, repository index, custom tools, update rollback safety.

## ≤ v1.2 (compact)

Foundations: sessions, streaming with sequence-stamped replay, tool
registry, downloader, installer, health checks, environment centre,
bounded authoritative run state, settlement edge.
