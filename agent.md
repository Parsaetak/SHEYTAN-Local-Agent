# SHEYTAN-Local-Agent — Agent Context (CURRENT v1.9.1 handoff)

This is the concise, current handoff for an engineering agent continuing
work on SHEYTAN-LA. It states what IS (verified), what is NOT (future), the
durable invariants, and the surfaces most sensitive to regression. Full
truth: `ARCHITECTURE.md` (architecture), `ROADMAP.md` (current/future
boundary), `changelog.md` (the ONLY release history), `worklog.md`
(session log).

**Current release: v1.9.1 — the live-stream observation-contract repair
(P0) + desktop runtime smoke gates, on the v1.9.0 feature surface.**
Version identity is exactly `1.9.1` everywhere (canonical gate:
`node scripts/release-version.mjs --check`). No stale 1.9.0
current-release claims exist outside `changelog.md`.

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
  desktop RUNTIME SMOKE (v1.9.1: real Wails binary launched under Xvfb,
  embedded UI + `/api/health` probed over loopback, clean shutdown) →
  ZIP verification. Nothing is skipped because the other platform is
  green.
* Windows x64 keeps its green release path (installer, execution
  evidence, discovery and data tests) plus the v1.9.1 RUNTIME SMOKE
  step: the real desktop executable is launched, its process liveness
  verified, the embedded UI + backend health probed over loopback HTTP,
  and the process shut down cleanly. Runtime smoke is a DISTINCT gate
  from build/package — packaging success never implies GUI runtime
  success.
* The Browser E2E fixtures run the REAL stack; no mock-only substitute
  for backend behavior is accepted. The zero-session reload flow and the
  v1.9 AI System / Goal UI flows are covered by
  `e2e/zero-session.spec.ts` and `e2e/ai-systems.spec.ts`.
