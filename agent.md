# SHEYTAN-Local-Agent — Agent Context (CURRENT v1.9.5 handoff)

This is the concise, current handoff for an engineering agent continuing
work on SHEYTAN-LA. It states what IS (verified), what is NOT (future), the
durable invariants, and the surfaces most sensitive to regression. Full
truth: `ARCHITECTURE.md` (architecture), `ROADMAP.md` (current/future
boundary), `changelog.md` (the ONLY release history), `worklog.md`
(session log).

**Current release: v1.9.5 — the v1.9.4 release-attempt repair: the
acceptance harness false-proof paths removed and the harness wired as a
REQUIRED Windows CI gate (P0), the loopback activity transport made
least-privilege and authenticated (P0: exactly one route —
`/ws/activity` — plus a per-server 256-bit capability token, strict
origin/host validation, visible desktop degradation on discovery
failure, `EnsureSetup` before the listener, single idempotent
`closeLoopback`), the engine/run admission TOCTOU closed atomically
(P1: the `llm.EngineAdmission` seam, deterministic 503 refusal with
transcript restore, race-detector green), and the tree-wide
identifier-free ordinary-log contract enforced (P1: `runId=`/`session=`
removed from every production log line; `gpuEvent` carries an ephemeral
`probe=` ordinal). Version identity is exactly `1.9.5` everywhere
(canonical gate: `node scripts/release-version.mjs --check`). No stale
1.9.4 current-release claims exist outside `changelog.md`.

**WHAT v1.9.4 CHANGED (the regression-sensitive surfaces):**

1. **Desktop loopback activity transport** (`internal/api/loopback.go`,
   `src/config.ts`, `src/store.ts`, `internal/api/ws.go`): the desktop
   process binds the EXISTING API handler on 127.0.0.1:0;
   `GET /api/ws-origin` returns the origin ("" = same-origin);
   `connectActivity` resolves it once via `ensureActivityWsOrigin()` and
   opens the socket through `openActivitySocket`. `allowedOrigin`
   accepts ONLY the exact Wails page origin (http/wails +
   `wails.localhost`) against LOOPBACK request hosts. Never broaden the
   origin check, never add a second streaming channel, never serve the
   socket from a second handler. The regression pins live in
   `internal/api/loopback_v195_test.go` (including a REAL WebSocket
   upgrade with the Wails origin receiving `attached`).

2. **Engine/run coordination gate** (`internal/llm/llama.go`
   `SetRunGate`/`ErrEngineBusyWithRun`, wired in `internal/api`
   `New`): `updateEngineVariantTx` refuses BEFORE anything happens when
   the engine is RUNNING and a run is active. The AUTO probe treats the
   refusal as a DEFERRAL (`internal/llm/variant_auto.go`) — never a
   persisted bounded failure. Do not move the gate after asset
   resolution and do not persist deferrals into gpu-probe state.

3. **Settled-reply proof selector** (`e2e/zero-session.spec.ts`
   `expectRealAssistantReply`): settled assistant replies render
   `.message-markdown` (MessageBubble → `<Markdown>`); `.message-content`
   exists ONLY on the live bubbles. Any future settled-reply assertion
   must target the markdown surface; never accept placeholder rows.

4. **Stream trace** (`src/stream-trace.ts`): opt-in via
   `localStorage SHEYTAN_STREAM_TRACE=1`, lengths only, bounded ring,
   `window.__SHEYTAN_STREAM_TRACE_DUMP__` exposed ONLY when enabled.
   The zero-session spec attaches it on failure. Keep the disabled path
   a strict no-op and keep content OUT of the trace.

5. **Run summary metrics** (`internal/api/server.go` settle):
   `gateMs`/`ttftMs`/`persistMs`/`acceptToDoneMs`/`totalMs` +
   `events`/`deltas`/`firstDeltaMs`. `publishMs` is retired — do not
   reintroduce it.

1. **Governor CPU policy contract** (`internal/governor/governor.go`,
   pinned by tests in BOTH directions): the envelope's CPU-reduction
   branch requires a SUSTAINED rolling signal — ≥ `cpuWarmupSamples` (3)
   folded samples AND rolling average ≥ `CPUReduceAbove`. A lone spike
   (including the FIRST sample ever folded, whose EWMA equals the raw
   value) never flips the envelope; sustained high load still reduces
   background/concurrency. The folded sample count rides
   `ResourceState.CPUSamples`. `Governor.SetCPUSampler` is a TEST seam —
   production keeps `governor.CPULoadPlatform` (ONE platform sampler);
   integration tests that assert a pressure contract MUST pin the seam
   (unknown or low CPU) or they will fold the host's real load again
   (the exact run 38035650428 failure mode).
2. **Native prefill parity gate** (`tests/test_prefill_parity.cpp`): the
   claims now match the proof exactly — per-position logits through the
   chunk=1 span cadence, final-token logits for chunks 3/7/16, full KV
   bytes across every chunking AND the serial run, edge/error bounds,
   mixed prefill→decode; tied output NOT claimed (no shipped fixture is
   tied). Any change to the layer math MUST keep that gate bit-exact or
   be a deliberate, documented numerics change.
3. **Zero-session / live-stream E2E contracts**: EVERY zero-session test
   now proves the settled assistant reply is real content (non-empty
   `.message-content`, no `[data-stream-placeholder]` descendant) via
   `expectRealAssistantReply`; the count-based row assertions alone are
   NOT sufficient (the generation bubble also carries
   `.message-row.from-agent`). Keep that helper on any new transcript
   test.
4. **UI typography floors** (`src/styles.css`): no `font-size` below
   10px (metadata floor), errors/essential states ≥ 11px, interactive
   control text ≥ 12px. Do not reintroduce sub-10px text; density comes
   from hierarchy and layout, not microscopic type.

**WHAT v1.9.2 CHANGED (carried forward):**

1. **Native engine forward path** (`native/engine/src/forward.h/.cpp`,
   `generate.cpp`, `tensor.h/.cpp`): the forward pass runs on a RESOLVED
   WEIGHT TABLE (once-per-binding `Weights::resolve`; rows walked by
   `row_ptr`/`dequant_row`) and prefill runs through `Forward::prefill_span`
   (16-token chunks, last-token-only logits, cancel cadence 16). Measured
   19.08 → 3.66 ms/token (5.2×). `Forward::token()` public semantics are
   UNCHANGED. Bit-for-bit parity is pinned by `tests/test_prefill_parity.cpp`
   (per-position logits via the chunk=1 cadence, last-token logits for
   the production chunk sizes, FULL KV bytes across every chunking) — any
   change to the layer math MUST keep that gate bit-exact or be a
   deliberate, documented numerics change. The native CMake build now
   DEFAULTS to Release when no build type is given.
2. **Desktop shell** (`internal/desktop/desktop.go`): the main window binds
   `events.Common.WindowClosing` → `app.Quit()` (wails v3.0.0-beta.16
   swallows webview-window closes on both platforms — verified in the
   dependency sources). Do not remove this binding: closing the window
   would strand a zombie process with the backend running.
3. **Desktop smoke gates** (`.github/workflows/build-desktop.yml`):
   Windows requires `MainWindowHandle != 0` + a true `CloseMainWindow()`
   return + exit code 0; Linux owns a real Xvfb display, finds the app's
   window via xdotool (`--pid` + `getwindowpid` ownership), closes it with
   `WM_DELETE_WINDOW`, and requires normal-path exit. `xdotool` is a Linux
   CI dependency. These gates are the authoritative GUI-runtime evidence —
   keep them honest (Kill remains a failing-run backstop only).
4. **Zero-session recovery** (`src/store.ts` `recoverRunFromIdle`): an
   idle sentinel WITHOUT a lastRun block that lands within 250 ms of the
   attach acknowledgement (`activityAttachAckAt`) is the attach handshake,
   never recovery evidence — the measured clobber mechanism. The v1.2.6
   Path A (idle + lastRun) and late-idle semantics are unchanged.
5. **E2E contracts**: the live-stream + zero-session visibility proofs
   observe REAL content only (`data-stream-placeholder` excluded); the
   zero-session observation bound is 60 s; live-stream failures attach
   compact structured diagnostics.

**THE CURRENT PROGRAM (v1.9.x):** complete the v1.9 boundary honestly —
see `ROADMAP.md` §"v1.9 current/future boundary": automatic sub-agent
fan-out from the goal runner, the protected-evaluation anti-hack guard,
MCP runtime wiring through the guarded registration pipeline, and
oversized-output spool triggers. Linux is a first-class release target:
the Linux CI path must reach Go verification → Browser E2E → stress →
build → package; nothing may be skipped because Windows passes.

---

## What SHEYTAN is

A local-first AI engineering laboratory — NOT an Ollama/LM Studio wrapper.
One Go runtime owns every authority exactly once; a React/TypeScript
frontend is embedded; managed llama.cpp and a native C++ engine path serve
inference behind one backend contract. Motto: **"The model proposes. The
tools execute. The laboratory verifies."**

## v1.9 capabilities (implemented, evidence in changelog §v1.9.0)

* **AI System object (`internal/aisystem`, `/api/systems`, System Centre
  UI).** ONE user-owned, revisioned run-configuration store: instructions,
  model override, reasoning preference, allowed tool surface, approval
  policy, skills surface, knowledge/memory/compaction policy,
  verification policy. Atomic per-document persistence, deterministic
  ordering, corruption tolerance, a reserved default system that
  reproduces the pre-v1.9 behavior exactly, non-destructive migration,
  clone/export/import. The ACTIVE system's snapshot (systemId +
  systemRevision) is frozen into every run BEFORE the run goroutine
  starts — mid-run edits can never mutate a running configuration.
* **Execution binding (v1.9).** The frozen snapshot applies server-side:
  instructions (one bounded system-prefix block), model override, effort
  preference on the EXISTING v1.8.5 ladder (low=0, mid=1024, high=4096,
  ultra=engine default; explicit request level wins), tool surface
  constrain (`ToolPolicy.AISystemConstrain` — offer AND execution, remove-
  only), skills filtering (excluded skills never inflate context),
  approval policy. Identity is observable: an `ai_system` activity event
  and the run response fields.
* **Goal engine (`internal/goal`, `/api/goals`, Goals UI card).** Durable
  long-horizon state: phases understanding → planning → acting →
  verifying → completed; waiting/exception states; bounded plan (≤12);
  per-step status/result/evidence; a goal-level evidence journal;
  checkpoints after planning, each completed step, approval boundaries
  and terminal settlement; resume WITHOUT replaying committed mutations;
  bounded replanning (≤2) that never erases failure evidence; turn
  budget parking; boot recovery that marks found-live goals paused
  (never falsely running) and is idempotent; terminal settlement from
  the orchestrator's OBJECTIVE verification report only — a model claim
  never completes a goal.
* **Approval authority (`internal/approval`, the `agent.WithApprovalGate`
  seam).** ONE deterministic risk classification (read-only /
  workspace-write / external-network / destructive / privileged);
  `auto` / `ask-risky` (default) / `ask-all`; the EXACT normalized call
  identity (`CallKey`) is the only thing an approval binds to; a bounded
  decision ledger; goal runs install a deny-by-default gate (no risky
  call executes silently); goal-level approvals persist durably and
  approve/resume the exact call; rejection is evidence; stale approvals
  are rejected. Chat/agent runs install NO gate — behavior unchanged.
* **Bounded delegation (`internal/multiagent/subtasks`).** Executable
  subtasks beside the advisory specialists: read-only parallelism ≤ 2
  (the concurrency seam can only tighten), mutating work serialized, no
  nested spawning, fan-out ≤ 8, deadlines are honest blocks,
  deterministic merge in `subtaskId` order, failures stay failed.
* **Repository navigation (`repo_nav`).** `open` / `navigate` / `read` /
  `grep` over identified sources through the SAME index and path-safety
  authority as `repo_search`; exact ranges, provenance, truncation
  state; entire files are never returned.
* **P0 repaired (the v1.8.8 Linux E2E blocker).** The zero-session
  reload-persistence defect: `run()` now sets its startup state BEFORE
  any transport work, the lazy `createSession({keepRunState:true})` can
  no longer clobber it, failure paths clean up deterministically, and
  the E2E waits for the deterministic run-dispatch marker plus durable
  post-reload transcript. Regression pair: `zero-session-send.test.ts`
  #6/#7.
* **P0 repaired (v1.9.1, the live-stream observation contract).** The
  CI live-stream growth assertion baselined on the live bubble's
  PRESENTATION PLACEHOLDER and spent its poll budget inside the engine
  gate + prefill phase (~40 ms/prompt-token measured; a 446-token
  briefing ≈ 8 s TTFT unloaded, >30 s under contention). Placeholders
  now carry `data-stream-placeholder` in the DOM; the live-stream
  proofs require real streamed content for the baseline and observe
  strictly-longer growth inside the genuine live window; the fixture
  budget is 320 tokens (40 real engine chunks — the deterministic
  snapshot count). No product streaming path changed; the chain was
  verified healthy end to end (WS frame + DOM timeline diagnostic).
* **Measured fact to carry (native engine path).** Native prefill is
  ~25–40 ms/prompt-token on a mid host (no batching yet); decode is
  130+ tok/s on the live fixture. First-token latency on the native
  path is dominated by prompt length until prefill batching lands
  (ROADMAP performance item).

## Verified foundations (v1.8 — unchanged authorities)

* **One run lifecycle** — one registry, one hub, one `runLive` per run;
  sequence-stamped activity replay; settlement ordering (outcome recorded
  before the terminal flip) through the ONE `finishSuccessfulRun` path.
* **Pause / edit / resume** — durable atomic checkpoints; transactional
  edits with journals + startup recovery; resume = semantic continuation;
  abort publishes `aborted`, never a fake `done`.
* **Streaming contract (do not regress)** — server bounded conflation-
  aware queue → newest cumulative snapshot; sequence/replay; reconnect;
  frontend fast path; triple-boundary self-healing flush; synchronous
  terminal flush (done/error/abort).
* **v1.8.5 reasoning ladder** — low/mid/high/ultra carry REAL numeric
  thinking budgets on the local llama.cpp request path; remote providers
  never receive the field; the native path documents the level as inert
  rather than faking it.
* **Execution/evidence invariants** — `GPU detected ≠ GPU available ≠ GPU
  selected ≠ GPU executed ≠ GPU verified`; identity-bound
  `ExecutionReceipt`; staged engine identity; one accelerator authority;
  one Governor (policy only — it never executes).
* **dataAnalysis (v1.8.7/v1.8.8)** — deterministic analyze/aggregate/
  join/quality/export; 256 MB input honesty; compact model-oriented
  output. Agent/Goal work should prefer it for data tasks.
* **Sessions/continuum/recovery** — one session store (hot cache, atomic
  writes, self-healing index); continuum chapter rollover; recovery
  handoffs for context exhaustion.

## The v1.9 runtime seams (where new code must hook)

* Run options (`agent.RunOption`): `WithAISystem`, `WithApprovalGate`,
  `WithToolPolicy`, `WithThinkingMode`, `WithRunIdentity`, … — new
  per-run behavior rides options, never globals.
* Tool policy: `ToolPolicy.allows()` is the ONE offer/execution gate; the
  AI System constrain binds FIRST (nothing can re-enable an excluded
  tool).
* Approval: classify through `approval.ClassifyRisk` only; never
  re-implement risk vocabulary.
* Goal drives: the drive executor is `goal.RunFunc` bound to the ONE
  orchestrator (`api.goalRunFunc`); no second model loop.
* Storage: new durable objects follow the house pattern (one JSON per
  object, unique-temp+rename, bounded, deterministic listing,
  corruption-tolerant).

## Invariants (all releases)

* One authority per concern — including the ONE AI System store, ONE goal
  store, ONE approval classification.
* No sleeps/timing hacks/retry storms; deterministic tests synchronize on
  authoritative state.
* Build/typecheck ≠ runtime proof; device detection ≠ device execution;
  model output ≠ objective evidence.
* No fake autonomy: everything stays bounded by permissions, approvals,
  workspace policy, the Governor, cancellation, pause and verification.
* No fake long-context: use the actual model/engine/effective context.
* No model-specific hard-coding; unsupported backend capabilities remain
  unsupported with honest fallback (never prompt-side emulation).
* The codename-removal gate stays enabled; release identity flows from
  package.json through `scripts/release-version.mjs` only.

## Linux / Windows evidence boundaries

* Linux CI path: source/frontend audit → Go verification (headless) →
  native engine build/tests → Browser E2E (real headless server + real
  engine + fixture GGUF) → stress → executable build → version smoke →
  desktop RUNTIME SMOKE (v1.9.2: a REAL Xvfb display the step owns, the
  app's real pid, the real top-level window located via xdotool with a
  getwindowpid ownership check, a real WM_DELETE_WINDOW close, and a
  normal-path exit — status 0) → ZIP verification. Nothing is skipped
  because the other platform is green.
* Windows x64 keeps its green release path (installer, execution
  evidence, discovery and data tests) plus the v1.9.2 RUNTIME SMOKE
  step: the real desktop executable is launched, a real top-level
  window is proven (`MainWindowHandle != 0`), `CloseMainWindow()`'s
  BOOL is checked (a delivered close), and exit code 0 is required
  through the normal path — with the exact unmet evidence level
  reported when no window exists. Runtime smoke is a DISTINCT gate
  from build/package — packaging success never implies GUI runtime
  success.
* The Browser E2E fixtures run the REAL stack; no mock-only substitute
  for backend behavior is accepted. The zero-session reload flow and the
  v1.9 AI System / Goal UI flows are covered by
  `e2e/zero-session.spec.ts` and `e2e/ai-systems.spec.ts`.
