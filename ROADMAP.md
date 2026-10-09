# SHEYTAN-LA Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA after the
2026-09 R&D reset. One rule governs the whole document: **future phases are
never marked complete because they are documented.** A capability is DONE
only when deterministic tests, race gates, integration or E2E evidence prove
it in this repository.

Artifact truth (single authority per artifact):

* `changelog.md` is the ONLY canonical release-history artifact;
* `README.md` describes the CURRENT product only (no release history);
* `UPDATE.md` carries the CURRENT release notes and maintenance behavior;
* `ARCHITECTURE.md` remains the architecture truth for what IS built.

Motto: **"The model proposes. The tools execute. The laboratory verifies."**

## Shipped baseline (compact)

**v1.8.8 is the shipped baseline; release evidence is in changelog.md.**

v1.8 delivered, with per-release evidence in `changelog.md`: the adaptive
runtime (task classification, tier system, context planning), the staged
native-engine program with execution receipts, GPU transaction authority,
the deterministic `dataAnalysis` authority, the zero-session send contract,
the pause/edit/resume run lifecycle, sessions/recovery/continuum machinery,
the Coding Lab, the scheduler, custom tools, the downloader/updater, the
Governor, and the skill stores. This roadmap deliberately carries NO detailed
v1.8.x release history — that history lives in exactly one place.

## Evidence labels used by the backlog

* **D — Documented/verified.** Supported by current repository behaviour,
  code, tests or CI. Can be acted on directly.
* **I — Architectural inference.** A reasonable implication of the current
  design, but NOT a measurement. Requires a measurement plan before
  implementation is trusted.
* **H — Hypothesis.** Requires profiling or an experiment before ANY
  implementation decision. Numbers attached to hypotheses are explicitly
  REJECTED as facts.

Only proven implemented capabilities may move from future to current; the
movement happens in this file together with the evidence pointer.

---

## v1.9 — AI System Builder + long-horizon agentic engineering — **CURRENT (partial; boundary below)**

v1.9 turns the runtime into the first real **AI System Builder** layer and
adds the durable long-horizon Goal engine, bounded delegation, the approval
authority, and deterministic repository navigation — always by EXTENDING the
existing one-authority runtime, never by duplicating it.

### Implemented in v1.9.0 (evidence-backed — D)

* **AI System object (first-class, user-owned).** `internal/aisystem` —
  durable store under `<DataDir>/ai-systems/` (atomic writes, bounded,
  deterministic ordering, corruption-tolerant), stable `systemId`, name,
  monotonic `revision`, instructions, model override, reasoning preference,
  allowed tool surface, approval policy, skills surface, knowledge/memory/
  compaction policy, verification policy, timestamps; CRUD + clone + export
  + import + activation with correct active-system fallback; reserved
  default system whose behavior is byte-identical to the pre-v1.9 runtime
  (safe non-destructive migration; fresh installs get exactly one valid
  default). HTTP: `/api/systems` (+ item/activate/clone/export/import).
  Tests: `internal/aisystem/aisystem_test.go` (fresh-install default,
  reload persistence, revision increments, freeze isolation, clone/export/
  import round trip, reservation, validation bounds, corruption tolerance,
  deterministic ordering, dangling-pointer repair).
* **Execution binding.** The ACTIVE system's snapshot is frozen into every
  run (`systemId` + `systemRevision`) before the run goroutine starts —
  a mid-run edit can never mutate a running configuration. The binding
  applies: instructions (system-prefix block), model override, reasoning
  preference (the EXISTING v1.8.5 ladder: low=0, mid=1024, high=4096,
  ultra=engine default; explicit request level wins), tool surface
  (`ToolPolicy.AISystemConstrain` — server-side, offer AND execution, never
  a prompt-side wish), skills surface (irrelevant/excluded skills never
  inflate context), approval policy. The frozen identity is published as an
  `ai_system` activity and travels on the run response. Tests:
  agent/api suites; frontend view-model tests.
* **Durable Goal engine.** `internal/goal` — goal state (goalId, original
  goal, system binding, phase, plan, current step, per-step status/result/
  evidence, evidence journal, changed files, verification, effort, turn
  budget, timestamps, terminal state) persisted per goal with atomic
  writes; phases `understanding → planning → acting → verifying →
  completed` with waiting/exception states (`waiting_for_approval`,
  `waiting_for_resource`, `paused`, `blocked`, `failed`); progress derives
  from actual plan state (no decorative percentages); checkpoints after
  planning, each completed step, approval boundaries and terminal
  settlement; bounded replanning (default 2) with the failure evidence kept
  at goal level (a plan revision can never erase the failure record);
  recoverable tool failures block the STEP, never destroy the goal; turn
  budget parks instead of running away; boot recovery marks any goal found
  live with no run behind it as paused (never falsely running); terminal
  goals stay terminal. The drive executor binds to the ONE orchestrator
  with the frozen AI System snapshot. HTTP: `/api/goals` (+ start/pause/
  resume/cancel/approve/reject). UI: the Goals card in the System Centre.
  Tests: `internal/goal/goal_test.go` (full journey, honest fallback plan,
  pause/resume without replaying committed mutations, cancel/reload,
  recovery idempotence, approval parking + stale rejection, bounded replan,
  turn budget, reload preservation of plan/approval/evidence, ordering).
* **Approval authority.** `internal/approval` — the ONE deterministic risk
  classification (`read-only`, `workspace-write`, `external-network`,
  `destructive`, `privileged/host-level`) from tool identity + normalized
  arguments; policy vocabulary (`auto`, `ask-risky` default, `ask-all`);
  exact normalized call identity (`CallKey`) with a bounded decision ledger
  (an approval is valid ONLY for the exact call); the per-run
  `agent.WithApprovalGate` seam (chat/agent runs install none — behavior
  unchanged; goal runs install a deny-by-default gate so no risky call
  executes silently). Goal-level approvals persist durably
  (`waiting_for_approval` + pending exact call survives reload; approve
  resumes the exact call; rejection is evidence; stale approvals are
  rejected explicitly). Tests: `internal/approval/approval_test.go`,
  goal approval tests.
* **Bounded sub-agents / delegation engine.** `internal/multiagent/subtasks`
  — executable delegation ALONGSIDE the existing advisory specialists:
  explicit budgets per subtask (tool surface, read/write capability,
  context, deadline), read-only parallelism ≤ 2 (the concurrency seam can
  only tighten, never widen), mutating work strictly serialized, no nested
  spawning (the executor is a leaf), bounded fan-out (≤ 8), deadlines are
  honest blocks, results merge deterministically in `subtaskId` order,
  failures stay failed with unresolved issues, parent receives concise
  structured evidence (never raw transcripts). Tests:
  `internal/multiagent/subtasks_test.go`.
* **Repository navigation.** `repo_nav` (`internal/repoindex/navigation.go`)
  — deterministic `open` / `navigate` (line/symbol context) / `read`
  (exact range) / `grep` (exact pattern, honest total match count) over
  identified sources; every result carries path identity, exact range,
  provenance and truncation state; entire files are never returned (line +
  byte bounds); all paths through the SAME path-safety authority as
  `repo_search` (traversal/absolute/UNC/volume forms refused identically
  on Linux and Windows). Registered in the runtime next to `repo_search`.
  Tests: `internal/repoindex/navigation_test.go`.
* **Progressive disclosure hardening.** The composer's skill activation
  filters through the AI System's enabled-skills surface; the tool surface
  constrain applies at offer AND execution; irrelevant skills never inflate
  the context (the v1.7 progressive markdown-skill disclosure is unchanged).
* **P0 — the zero-session reload persistence defect (Linux E2E blocker).**
  Root cause: `run()`'s lazy zero-session `createSession()` executed while
  the composer still read idle — an enabled-composer observation could
  legally sample INSIDE the startup window and reload before
  `POST /api/run` was dispatched (session existed server-side pending;
  transcript permanently empty). v1.9.0 sets the run-startup state BEFORE
  any transport work, makes the lazy create preserve it
  (`keepRunState`), cleans the state on every failure path, and
  snapshots attachments before transport. The E2E now waits for the
  deterministic "run left the client" marker (the optimistic user bubble)
  plus settlement before reloading, and asserts the durable transcript
  (user AND assistant rows) after reload. Deterministic regression pair:
  `src/zero-session-send.test.ts` #6/#7.

### v1.9 current/future boundary (NOT implemented — future work, D-planned)

* Automatic sub-agent fan-out FROM the goal runner (the delegation engine is
  implemented and tested; the goal loop does not yet decompose steps into
  subtasks automatically).
* Protected-evaluation anti-hack guard (spec §12): deterministic detection
  of protected reference/answer artifact access around Lab evaluation
  contexts — designed, not implemented; opt-in and context-aware when it
  lands.
* MCP runtime wiring (the `internal/mcp` client remains implemented and
  tested but not yet registered into the tool registry; the guarded
  registration pipeline exists — the v1.9 constraint policy above already
  covers MCP-class risk classification for when it lands).
* Oversized-output spool handles for ordinary shell/tool results (the
  artifact/task-registry machinery exists since v1.7; the automatic spool
  trigger for arbitrary outputs is future work).
* Native-engine PREFILL BATCHING (performance, evidence-backed — measured
  in the v1.9.1 session): the native engine currently processes prompt
  tokens one forward pass at a time (~25–40 ms/prompt-token measured on a
  2-core host; a 446-token system briefing ≈ 8 s of TTFT unloaded, >30 s
  under CPU contention; decode on the same fixture is 130+ tok/s). Batched
  prompt processing is the highest-leverage native-path latency item — it
  bounds first-token latency for every chat/agent/goal turn. Not started;
  the measurement method (standalone engine-host probe + run telemetry
  TTFT/classifyMs) is recorded in worklog §v1.9.1.

## v1.10 — Universal Agent — **FUTURE — NOT IMPLEMENTED**

Controlled computer-use layer, all through explicit permission/trust
boundaries: browser and desktop interaction, screen, mouse/keyboard,
application discovery, document workflows, multimodal execution,
connectors, voice foundation. Additionally:

* bounded parallel tool orchestration with a deterministic execution graph;
* tool-call state hashing for repeated-state/cycle detection — evidence-
  gated, no fixed retry count until experiments establish that policy;
* bounded reflection/recovery and pause/cancel propagation through the run
  lifecycle;
* artifact/handle workflows (consume the large-output handles).

Never replace safety or objective verification with a circuit breaker alone.

## v1.11 — AI Learning & Evaluation — **FUTURE — NOT IMPLEMENTED**

Evaluation suites, baselines, experiments, candidate variants, diagnostics,
feedback learning, skill evolution, memory optimization, routing/context
optimization, promotion/rollback, improvement history; representative
local-model benchmark/evaluation suites; the chat-template compatibility
matrix as living evidence; performance regression gates; candidate
promotion/rollback decided ONLY by measured evidence. The v1.9 protected-
evaluation guard is a prerequisite for trustworthy evaluation runs.

Honesty gate: no system change becomes an "improvement" merely because the
model says so — only measured evaluation evidence promotes a candidate.

## v1.12 — Advanced Model Intelligence — **FUTURE — NOT IMPLEMENTED**

Multi-model routing, capability discovery, specialist models,
vision/reasoning routing, draft/speculative models, dynamic loading and
unloading, model pools, cross-model fallback, resource-aware selection;
native-engine optimization only with numerical parity tests; optional local
embedding/vector infrastructure only when RAG measurements justify it.

No specific external stack is mandatory for any of this.

## v2.0 — SHEYTAN Local AI Platform — **FUTURE**

Strategic end state: a complete user-owned local AI platform where users
create, run, understand, modify and continuously improve durable AI systems
while keeping ownership of their models, knowledge, memory, skills, tools,
behavior, experiments and versions; scalable local workers, mature
multi-run orchestration, cross-platform support, optional multi-process/
remote workers, mature observability/evaluation, portable import/export/
versioning (the v1.9 AI System export/import is the first portable unit).

---

## Performance measurement framework (applies to ALL backlog work)

Use the EXISTING RunClock/performance surfaces where they already exist.
Track, where measurable: classification duration; context duration; prompt
preparation; serialization; request sent; first byte; TTFT; generation
duration; first browser-visible streamed update; WebSocket bytes; tool
execution duration; verification duration; persistence duration; total
turn time. Add a measurement ONLY when it supports a real optimization —
never as decoration.

Long-running stress dimensions already exercised by the suite (extend, do
not weaken): 5k+ token streaming; long reasoning; hundreds of session
messages; large tool outputs; large repositories; repeated Lab repair
loops. No numeric targets are asserted anywhere in this roadmap without a
measurement behind them.

---

## Engineering invariants (all releases)

* One authority per concern: one run lifecycle, one session store, one tool
  registry, one scheduler, one downloader, one installer/update path, one
  engine lifecycle, one memory authority, one runtime policy authority, one
  AI System store, one goal store, one approval classification.
* No sleep-based correctness; deterministic tests synchronize on
  authoritative state and events.
* Build/typecheck ≠ runtime proof; CI ≠ physical-PC runtime proof.
* Hardware detection is never reported as execution evidence; GPU/NPU
  claims stay separated (detection → backend availability → device
  enumeration → real execution).
* Numeric/unsupported engine arguments fail before process spawn; the
  modern llama.cpp CLI contract (b10642/b11205) is verified against the
  binary `--help` fixtures.
* The codename-removal gate stays enabled and exact on tracked trees and
  clean-room drops.
* Performance claims require measurements made on THIS codebase; numbers
  from external reports are hypotheses (H) until reproduced here.
* Architecture proposals that replace the Go runtime, the one-authority
  session/run/tool/memory design, or the objective-verification pipeline
  are rejected by default — changes to those require evidence-gated
  migration plans, not rewrites.
