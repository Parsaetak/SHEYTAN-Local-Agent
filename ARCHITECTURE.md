# SHEYTAN-Local-Agent — Architecture Truth Table

Current for **v1.9.2**. This document states the architecture that IS
implemented and verified in this repository. Future R&D is NOT described as
implemented; the roadmap (`ROADMAP.md`) owns the future. Historical
release-specific architecture notes were consolidated at the end.

Motto: **"The model proposes. The tools execute. The laboratory verifies."**

---

# Part I — The single-runtime architecture

## 1. One process, one runtime stack

One Go process owns the runtime (`internal/runtime` `Stack`). The stack
constructs and owns every authority exactly once:

* **Engine lifecycle** — managed llama.cpp servers and the native C++ engine
  are adapted to one `llm.Backend` contract; the selection policy picks the
  native path when the user selected it AND it can generate, otherwise the
  llama fallback. Start/restart/stop/upgrade run through ONE transactional
  maintenance gate (identity manifest + committed state; two-boot
  idempotency is deterministic).
* **Run lifecycle** — one runs registry per session (`internal/api`
  `runState`/`runLive`). Fresh and resumed runs share the same registry
  entry, hub and authoritative state. Pause → resume keeps ONE runId.
* **Session store** — one store (`internal/sessions`), one hot-cache the
  memory manager bounds.
* **Tool registry** — one registry (`internal/tools`) with generation-aware
  schema caching; custom tools sync into it (no second registry).
* **Scheduler / automations** — one scheduler (`internal/scheduler`).
* **Downloader / updater / installer** — one download authority, one
  update/installer path with the authoritative engine-variant resolver
  (explicit variant requests never fall back silently).
* **Memory authority** — one memory manager (`internal/memmanager`) with
  registered trimmers, run-boundary tracking and cooperative cleanup.
* **Runtime policy authority (v1.8.0)** — one Governor
  (`internal/governor`), policy only (§Governor below).

## 2. Engine / backend architecture

llama.cpp remains the reference backend: managed server process, modern CLI
contract (split `--cache-type-k` / `--cache-type-v`, `--flash-attn
on|off|auto`), strict numeric-argument validation that FAILS BEFORE process
spawn, device enumeration as the only execution-evidence source. The native
engine (C++, `native/engine`) is a supervised alternative path with real
generation for validated llama-architecture models and honest incapability
verdicts (fallback, never a crash). Engine identity, corruption rows and
update transactions are deterministic and covered by fault-injection
(v1.8.1: a rolled-back transaction also restores the recorded tag from the
restored manifest, so state, manifest and serving binary can never
disagree).

**Native forward path (v1.9.2):** the forward pass owns a RESOLVED WEIGHT
TABLE built once per model binding (`Forward::init` → `resolve_model`):
every layer's seven weight matrices and the output projection are resolved
through the tensor layer's `Weights::resolve` (name lookup, shape and type
validation, whole-tensor byte-range check — the same bounds authority as
the per-row path, executed ONCE), and the forward pass then walks rows by
pointer arithmetic (`tensor::row_ptr` + `tensor::dequant_row`, one shared
dequantization switch). Prefill runs through `Forward::prefill_span` in
bounded 16-token chunks with the vocabulary logits projection ONLY for the
final prompt token; cancellation is observed between chunks at the
documented cadence. Numerical identity with the serial `Forward::token()`
path is pinned bit-for-bit (last-token logits + full KV bytes) by the
native `test_prefill_parity` gate across chunk sizes 1/3/7/16. The native
CMake build defaults to the Release configuration when the caller does not
choose one (an unflagged -O0 engine is not a supported inference build).

**Desktop shell lifecycle (v1.9.2):** the native Wails window binds
`events.Common.WindowClosing` to `app.Quit()` — verified against the
`wails v3.0.0-beta.16` sources, where a webview window's platform close
only emits the event and destroys the window while the application event
loop keeps running with zero windows on both Windows and Linux. The
single-window product therefore terminates its lifecycle on the visible
close path, and the deferred `srv.Close()` cleanup runs.

**Model capability cards (v1.8.1):** the GGUF header parser reads up to
32 MiB of metadata (`ggufMetadataReadLimit`) — real Gemma-class tokenizer
blocks (262,144-entry arrays) exceed the old 8 MiB bound and made the card
unreadable. A card that still cannot be read resolves to a nil capability
object, and every consumer follows the documented fallback (configured
context, conservative estimator) — never a dereference. The capability
resolution itself is uniformly nil-config-safe.

**Execution/evidence ladder (v1.8.5, the Phase 2 boundary contract):**
ONE shared structure (`internal/llm/execution.go`, surfaced as the
`execution` block of `/api/engine`) composes the existing authorities —
device detection, backend health, the accelerator selection memo, verified
model loading, measured generation telemetry, runtime offload lines — into
the monotone ladder `detected → backend-available → device-selected →
model-loaded → generation-executed → execution-evidence → verified`. The
composer is PURE over explicit inputs; the stage stops at the first
unproven rung and names the gap, so device enumeration can never equal
verified execution. Go stays the control plane; the C++/serving backend
stays the execution plane; Phase 2's deep engine work consumes exactly
this contract.

**Execution truth enforced end-to-end (v1.8.6):** the ladder's inputs are
now TRUTHFUL at the source. The ONE accelerator authority
(`internal/accelerator`) enforces
`GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
verified`: `--list-devices` enumeration SELECTS GPU_VULKAN with the
pending-execution verification plan and the CPU safety net; the
`ExecutionVerified` verdict requires the measured runtime offload line or
a still-valid `ExecutionReceipt` — a structured identity-carrying object
(kind, line, engine tag, variant, device, model, status) whose
`ValidFor(engineTag, variant)` check rejects stale receipts (another
engine build, another variant, a failed bounded probe). The LAUNCHER
(`LlamaServer.autoGPUOffload`) activates AUTO `--n-gpu-layers` only on
PROVEN execution (the current boot's offload line, or a persisted
verified GPU-probe receipt with a matching engine identity); enumeration
and DLL presence keep AUTO CPU-safe. The bounded candidate transaction
keeps a strictly transaction-scoped proving mode
(`gpuCandidateProving` inside `updateEngineVariantTx`, deferred reset)
where enumeration MAY enable offload to prove the candidate — commit
still requires a real generation AND the measured offload line. The
offload evidence is PER-BOOT (`launchArgs` resets it — a restart or model
swap re-proves). `/api/engine` and `/api/perf` consume the ONE accessor
(`currentAcceleratorResolution`): the resolution memo is validated
against its input signature (engine tag + variant + profile + model +
evidence + probe state) and recomputed when stale, so the engine surface
never depends on the perf poll order and a stale poll-only snapshot can
never claim a current backend.

**Resource integration (v1.8.6):** the Governor's resource state folds
the CURRENT inference workload (an injected footprint source on the
Stack: model file bytes as a FILE fact + planned KV at the serving
window, from the existing model-card/context authorities); the envelope
accounts for a footprint that consumes the resident budget (background
reduced through the existing honest adjustment class, reason stated);
the run gate (`Stack.GovernorAdmitsModelLoad`, consulted by EnsureLLM
and EnsureLLMContext before any engine start) admits model loads against
the Governor's measured envelope using the same resident plan preflight
computes — sustained pressure defers with the explainable reason, an
unmeasured Governor falls through to the preflight gate. The engine
process RSS is MEASURED (`LlamaBackend.Metrics` via `resources.ProcRAM`)
and flows into the Governor's engine facts; `/api/perf`'s `engineMemory`
block serves the engine's memory evidence with provenance labels
(measured RSS / model FILE size never "RAM used" / offload line / KV
named unknown where no measured surface exists). The Windows CPU seam
(`governor/cpu_windows.go`) measures real load via kernel32
`GetSystemTimes` over the ONE shared priming/delta state machine
(`governor/cpu_delta.go`) — no second sampler, no second cadence; Linux
stays on `/proc/loadavg`.

**Reasoning-depth budgets (v1.8.5):** the composer's four-level control
(low/mid/high/ultra) carries a numeric thinking-token budget onto every
generation request (`llm.ChatRequest.ReasoningBudget` → the llama.cpp
request-level `reasoning_budget_tokens` parameter, verified in BOTH
managed builds' server sources). low = 0 (thinking off), mid = 1024
(bounded default), high = 4096, ultra = not sent (engine default). Local
engines only; a non-thinking model ignores the budget (reasoning is never
fabricated); the native path has no budget control and is documented-inert.
The persisted Show/Hide Thinking preference is VISIBILITY ONLY — it never
enters the request.

## 3. Session / run lifecycle

A run is: POST /api/run → registered (one registry entry, one hub, one
`runLive`) → phases preparing/thinking/generating → terminal done/error/
aborted → registry release. The publisher stamps every activity with a
monotonic per-run sequence and folds it into `runLive` BEFORE broadcast;
WebSocket replay is snapshot-at-seq-N plus events > N (gapless,
duplicate-free).

**Run control (v1.7.4–v1.8.0):** pause requests land at safe boundaries
(stream deltas, between tools, round tops) — the orchestrator returns a
paused result; the checkpoint (durable, atomic, integrity-bounded) is
persisted BEFORE the state says PAUSED. Edits are transactional: CAS
validation, transcript replace, checkpoint commit, publication strictly
last, with durable journals and startup recovery converging every
interrupted transaction from ACTUAL durable evidence (v1.7.6). Resume
rebuilds the continuation from the transcript authority; the accepted draft
rides the request; the settlement is the SAME `finishSuccessfulRun` path a
fresh run uses. **v1.8.0 synchronization contract:** after a resume, proof
of a live resumed generation requires a strictly newer sequence AND a
changed cumulative response/reasoning snapshot
(`resumedGenerationEvidence`) — stale cumulative state is never evidence.
**Abort honesty (v1.8.0):** the orchestrator's context-cancelation exits
publish the `aborted` activity type; the live state and the outcome
registry agree on `aborted` forever (settleTerminal never has to override
an early flip).

**Streaming display (v1.8.2; v1.8.4 self-healing):** the backend emits
cumulative `response`/`reasoning` snapshots (SmoothStream, ~8 ms minimum emit
interval). In the frontend, stream-critical events fold into the streaming
accumulator AT SOCKET-RECEIVE TIME (`src/stream-fast-path.ts`). The flush
is scheduled on THREE deterministic boundaries
(`src/stream-flush-scheduler.ts`) — a REUSABLE MessageChannel macrotask, a
0ms timeout task, and an animation frame; whichever runs first flushes and
the others no-op — so visibility and run settlement never depend on any
single scheduling primitive (the WebView2 failure classes: rAF suspension,
and a lost MessageChannel delivery which permanently wedged the v1.8.2
one-shot controller). The task controller is recoverable by construction:
latest-callback-wins, an explicit in-flight handshake, and a deterministic
microtask fallback the moment a post arrives while a message is still
undelivered. The ACTIVITY flush (statuses, tool events, done/error/aborted
lifecycle events) uses the same scheduler — the v1.8.2 design left it on a
rAF-only schedule, so a suspended frame callback starved run settlement.
Exactly ONE coalescing latch remains; the events still join the activity
timeline batch, a self-draining ledger guarantees the batch never processes
them twice, and cumulative replace semantics, replay/reconnect idempotence,
sequence/stale-run protection and the synchronous done/error/abort flush
are unchanged.

**Subscriber delivery (v1.8.5):** the run hub's per-client delivery is a
bounded, CONFLATION-AWARE queue (`activitySub`, `internal/api/server.go`)
— the v1.2.x buffered channel dropped the NEWEST event on overflow, which
for cumulative snapshots discarded the frame carrying the full text while
the buffer kept stale prefixes (a backpressured transport therefore froze
visible text until the run ended — the server-side twin of the v1.8.4
MessageChannel wedge). Overflow now evicts the OLDEST conflatable event
(response/assistant_delta, reasoning/thinking_delta, status — the kinds
whose newest frame subsumes older ones); order and the sequence replay
contract are preserved; terminal events are never preferentially evicted;
the publisher never blocks; every event write carries a generous deadline
so a wedged client tears down deterministically and recovers through the
reconnect snapshot replay.

**Live memory evidence (v1.8.2):** the orchestrator composes
`contextplan.MemoryEvidence` from the survival-reconciled injection facts
and publishes it on the existing `context` activity. The live bubble
renders exactly that record (`src/memory-evidence.ts`); a block the
windower elided is never claimed, and no record means no indicator.

**Capability self-model (v1.8.2):** a deterministic intent signal
(`taskclassify.Signals.SelfDescribe`) triggers ONE bounded runtime
self-model block (`internal/agent/selfmodel.go`) composed from the
existing authorities — the registry snapshot, the already-resolved model
card, config-backed backend facts, the sysinfo fast snapshot. No second
capability database exists; capability questions trigger no research,
recall or repo indexing.

**Model capability cache (v1.8.2):** `internal/llm/modelcaps.go` caches the
immutable parsed GGUF card under (path, size, mtime) with NO TTL;
config-sensitive fields (multimodal pairing, recommendations) re-derive
from the cached card when a configuration fingerprint changes. A turn
separated by minutes no longer re-parses the GGUF metadata.

**Log redaction (v1.8.2):** `internal/logging/redact.go` removes the
opaque identity tokens (`runId=`, `session=`, …) at the ONE central sink;
internal identity (API objects, run state, journals, storage keys) is
untouched.

**Session-list generation authority (v1.7.5; init consumer v1.8.3):** every
session-list write in the frontend — `refreshSessions` AND the startup
`initializeAgentOnce` — goes through ONE monotonic generation counter
(`src/session-list-guard.ts`). A response may only land while its ticket is
current; every mutation (create/delete/rename/mode switch) invalidates the
tickets taken before it. A failed DELETE surfaces through the store error
state (never a fake success), and the created-session prepend is idempotent
by id.

**Zero-session Send (v1.8.4):** a space with zero sessions is a FIRST-CLASS
state, not a dead end. The composer stays usable (the model gate and the
live-run gate keep their semantics), and pressing Send runs the store's
lazy `createSession()` — creating a session in the CURRENT mode, making it
active, and continuing the SAME send through the normal run lifecycle
(deterministic store-level suite + real-stack browser E2E in both modes,
including reload persistence). `deleteSession` already produced a valid
zero-session state; the v1.8.3-era composer gates made it unreachable.

**Context-refresh generation authority (v1.8.4):** `sessionContext` is
written ONLY by the newest context request for the still-active session.
Every `refreshSessionContext()` claims a strictly increasing generation;
a response may land only while it is both the newest request and bound to
the active session. Session switch, deletion (the visible context is
cleared and a replacement session's is refreshed), creation, mode switch
and fresh-run transitions invalidate every in-flight response. The backend
remains the ONE context authority — this is purely response ordering; the
UX stays automatic/unlimited with no user-controlled context-size controls
(`src/context-refresh-race.test.ts` forces every interleaving by
causality).

**Engine identity during the deferred-commit window (v1.8.4):** the
transactional installer writes a window-scoped staged-identity marker
(`engine-stage-pending.json` inside the managed bin directory) the moment
a byte-verified candidate is swapped in, and clears it at Commit (the
install manifest becomes the authority) and at Rollback (the previous
package is restored). While the marker exists, the boot path
(`detectCapsForBoot`) derives the identity from it: the ACTUAL staged
binary is probed, capability-cached and reported — never the recorded tag
left over from the previous build (`updater.StagedEngineIdentity`;
`internal/updater/staged_identity_v184_test.go`).

**GPU launch posture contract (v1.8.4):** `gpuAutoOffload` is the AUTO
offload posture (default true; the launch-time evidence gate
`autoGPUOffload` decides from device enumeration/offload evidence — CPU
while nothing is proven). `gpuAutoOffloadUserSet` marks EXPLICIT user
actions (settings toggle, direct config patch); the recommendation
pipeline NEVER writes a derived OFF (the pre-1.8.4 derived
`false + numGpu=0` permanently blocked the AUTO Vulkan candidate — the
stale state is repaired once at Load, honestly noted and persisted, and
never touches an explicit user OFF, a manual layer count, or a CPU
requested profile). The AUTO candidate eligibility gate
(`internal/api/gpu_autoprobe.go`) therefore sees only a genuine explicit
OFF. Evidence ladder unchanged: detection ≠ enumeration ≠ offload
evidence.

**Log redaction (v1.8.2):** `internal/logging/redact.go` removes the
opaque identity tokens (`runId=`, `session=`, …) at the ONE central sink;
internal identity (API objects, run state, journals, storage keys) is
untouched.

The session-delete contract (pending and persisted deletion, replacement
selection, stale-response non-resurrection, mode separation, honest
duplicate-delete) is pinned deterministically at the store level
(`src/session-delete-regression.test.ts`), the Go store level
(`internal/sessions/delete_pending_test.go`) and the browser level
(`e2e/sessions.spec.ts`), with mutation-verified coverage.

## 4. Memory / context architecture

Per-session context policy (1.1.6), planned context windows, bounded
snapshots (256 KiB) and a bounded recent-events ring; durable summaries;
cross-mode history references; recall indexing; continuum rollover. The
memory manager bounds caches (sessions hot cache, image cache) with
registered trimmers, tracks run boundaries and performs cooperative
cleanup — never process killing.

## 5. Tool / skill / scheduler authorities

One tool registry with deep-copied metadata (race-safe), generation-aware
spec cache; skills as a persisted load-on-demand store; automations via the
one scheduler; research as ONE service behind the research tool; the coding
lab with isolated workspaces, repair and verifier loops; repository
indexing with hybrid search.

The data authority is the ONE `dataAnalysis` tool (v1.8.7): CSV/TSV/JSON
loading with type inference (JSON column order = document key order,
deterministic), a byte-budgeted LRU dataset cache and parse-once numeric
columns, and the deterministic actions `analyze` / `aggregate` / `join` /
`quality` / `export` on top of the existing profile/stats/correlation/
filter/sort/query/histogram family. Every analysis result is a pure
function of the dataset (groups key-sorted, joins file-order, findings in
column order) and carries provenance metadata (backend, bytes, rows,
cols). The backend is the in-process pure-Go engine with an honest
256 MB input bound; DuckDB (cgo, static) was evaluated and rejected for
packaging/CI complexity, so there is no SQL/Parquet surface and no
streaming claim — large work goes through filter/aggregate first.
Tier-2 engine discovery (`internal/engdiscovery`) is likewise
deterministic since v1.8.7: a priority/level barrier retains candidates
in (level, path) order and seals the scan at level drain, so the winner
never depends on worker completion order; cache-parent seeds are
deferred until the class-1 band so repeated scans keep the priority
contract.

## 5b. v1.9 authorities — AI System, Goals, approvals, delegation, navigation

**AI System (`internal/aisystem`, the ONE store).** A user-owned,
revisioned run-configuration object: instructions, model override,
reasoning preference, allowed tool surface, approval policy, skills
surface, knowledge/memory/compaction policy, verification policy.
Persistence is the house pattern (one JSON per system under
`<DataDir>/ai-systems/`, unique-temp+rename atomicity, bounded counts,
deterministic (CreatedAt, systemId) ordering, corruption-tolerant
listing) with a persisted active pointer and dangling-pointer repair.
The reserved `default` system reproduces the pre-v1.9 behavior exactly;
migration is non-destructive; import mints fresh identities (never
overwrites); deleting the active system falls activation back to
Default.

**Per-run snapshot binding.** `handleRun` resolves the binding ONCE —
explicit request `systemId` wins (404 when missing), otherwise the
active system — and freezes a VALUE snapshot (`systemId` +
`systemRevision`) into the run before the goroutine starts. The binding
is enforced at five server-side seams: instructions ride the system
prefix as one bounded block; the model override feeds every request
build; the reasoning preference resolves at the wire boundary through
the EXISTING v1.8.5 numeric ladder (explicit request level wins); the
tool surface constrains offer AND execution (`ToolPolicy.
AISystemConstrain`, checked first inside `allows()` — remove-only, and
the Net Search intent is equally subject to it); the skills surface
filters skill activation in the composer. The frozen identity is
published as an `ai_system` activity and echoed on the run response.

**Goal engine (`internal/goal`, the ONE store).** Durable per-goal
documents (`<DataDir>/goals/`, same atomicity/ordering/tolerance
discipline) hold the phase (understanding → planning → acting →
verifying → completed), status (active / waiting_for_approval /
waiting_for_resource / paused / blocked / failed / completed /
cancelled), bounded plan, per-step status/result/evidence, a goal-level
evidence journal, changed files, verification state, effort, turn
budget, replans and checkpoints. The drive loop (`goal.Drive`) executes
ONE run per phase segment through the injected `RunFunc` — bound in
`api.goalRunFunc` to the ONE orchestrator with the frozen AI System
snapshot and the approval gate — and persists a checkpoint after
planning, each completed step, approval boundaries and terminal
settlement. Resume continues from the checkpoint (no replay of committed
mutations); replanning is bounded and keeps failure evidence at goal
level; the turn budget parks; `RecoverOnBoot` marks found-live goals
paused (idempotent; terminal stays terminal). Terminal settlement maps
the orchestrator's OBJECTIVE `VerificationReport` — a model claim never
completes a goal.

**Approval authority (`internal/approval`).** One deterministic risk
classification (read-only / workspace-write / external-network /
destructive / privileged-host-level) from tool identity + normalized
arguments (shell-fragment classification for destructive/privileged;
conservative default for unknown tools). Policy vocabulary: `auto`,
`ask-risky` (default), `ask-all`. The EXACT normalized call identity
(`CallKey` = tool + risk + canonical args) is the only binding for a
decision; the bounded ledger keeps approvals call-exact. The gate is a
PER-RUN seam (`agent.WithApprovalGate`) — installed only by goal runs
(deny-by-default so no risky call executes silently); chat/agent runs
install none, preserving their behavior byte-for-byte. Goal-level
approval parking is durable (`waiting_for_approval` + pending exact
call survives reload; approve resumes the exact call; rejection is
evidence; stale ids are rejected).

**Bounded delegation (`internal/multiagent/subtasks`).** Executable
subtasks beside the advisory specialists: per-subtask budgets (tool
surface, read-only capability, context, deadline), read-only
parallelism ≤ 2 (the concurrency seam can only tighten), mutating work
serialized, fan-out ≤ 8, leaf executors (no nested spawning), honest
blocks on deadline, DETERMINISTIC merge in `subtaskId` order, failures
stay failed.

**Repository navigation (`repo_nav`).** `open` / `navigate` / `read` /
`grep` over identified sources through the SAME index and the SAME
path-safety authority as `repo_search` (`relWithinRoot`); bounded
ranges (120 lines / 8 KiB / 1 MiB file ceiling), provenance per result,
honest total match counts and truncation flags; entire files are never
returned.

## 6. Evidence model

Evidence classes, always named: deterministic unit · race gate ·
integration · E2E · CI · real-engine probe · real-host runtime · known
limitation. Build/typecheck is not runtime proof; CI is not physical-PC
runtime proof; hardware detection is not execution evidence (GPU/NPU:
detection → backend availability → device enumeration → real execution,
each a separate verified surface). Fault-injection panics in
edit-transaction tests are intentional and stay.

---

# Part II — §Governor: the v1.8.0 Runtime Governor

## The loop

```
Telemetry (existing preflight.LiveMonitor — one sampler, one cadence)
  → ResourceState (measured facts + explicit unknowns)
  → Pressure model (monitor hysteresis + sustained duration + rolling signals)
  → Envelope (admit / reduce posture + adjustment class)
  → Runtime decision (admission verdicts with reasons)
  → Existing subsystem actions (execution stays with the owners)
  → Measured outcome (next sample feeds back)
```

The Governor owns POLICY. It executes nothing: the monitor keeps the
critical-protection path (cooperative cancellation of active runs), the
engine lifecycle owner keeps engines, the scheduler keeps automations, the
memory manager keeps trimming. There is no second sampler, no second
scheduler, no telemetry database.

## Resource state

One coherent snapshot (`governor.ResourceState`): RAM total/available
(`availableKnown` gates trust), process RSS, engine RSS where the engine
lifecycle owner can measure it, active runs (from the run authority), CPU
load where the platform seam can measure it, the accepted pressure level,
sustained duration, and an explicit unknowns list. Nothing interpolated.

## Pressure model

The shipped four-level vocabulary is REUSED (ok / warning / high_pressure /
critical_pressure) as the evidence-backed mapping of the R&D's
GREEN/YELLOW/ORANGE/RED/EMERGENCY — no second scale. Sample hysteresis
belongs to the monitor (unchanged). The Governor adds the TIME dimension:
sustained-duration tracking per level and bounded rolling signals (EWMA,
30-sample ring) so one noisy sample never flips policy.

## Envelope

The envelope states what the runtime may do NOW: `admitHeavyweight`,
`reduceBackground`, `reduceContextWork`, `reduceToolConcurrency`, the
resident admission budget, the strongest required `AdjustmentClass` and
deterministic reasons. Adjustment classes are honest about application
semantics: `live` (a real live control point exists), `next-run` (the next
generation picks it up), `reload` (the engine must reload — the llama.cpp
contract is not pretended away).

## Admission

Heavyweight admission is the coarse pressure gate (refused at high/critical,
conservatively refused when available RAM is unknown). Model/workload
admission (`AdmitMemory`) requires a caller-supplied RESIDENT plan (weights
+ KV + overhead — from the existing model-card/context-planning
authorities); mapped file size is never resident size; the resident budget
is available RAM − host headroom − measured process RSS (engine RSS is not
subtracted twice — it already shrank available RAM); plans without an
estimate are refused; every verdict carries its arithmetic in the reason.

## Self-model and API

`SelfModel()` reports verified facts with provenance. `GET /api/governor`
composes ONE read model from the existing authorities — governor state +
envelope + self-model, hardware (sysinfo authority, detection-level GPU/NPU
labeled as detection), engine health (existing `Metrics`), capabilities
(what the process actually constructed). The System Centre's Runtime
Governor card renders it — real values, unknowns named, no decorative
graphs.

## Tests (evidence)

* deterministic unit: the full policy matrix (state folding, unknowns,
  sustained warning, envelope per level, rolling decay, admission matrix,
  budget arithmetic, adjustment classes, self-model schema, concurrency)
  — `internal/governor/governor_test.go`, run under `-race`;
* integration + API contract: `/api/governor` composition, version
  identity, method guard — `internal/api/governor_test.go`;
* pause/resume synchronization: deterministic unit contract + integration
  replay of the CI scenario — `internal/api/run_control_v180_test.go`;
* abort honesty: state/registry agreement + `observe(aborted)` folding —
  same file. Known limitation: CPU policy is inert on Windows until a live
  Windows load authority exists (reported as unknown, never guessed).

---

# Part III — Compact history (pre-v1.8 architecture notes)

The architecture above is ACCUMULATIVE: v1.8 added the Governor without
moving any authority. The load-bearing historical layers, compressed:

* **v1.2.x** — authoritative run state with sequence-stamped replay; the
  settlement edge (outcome recorded BEFORE the terminal flip); bounded
  registries.
* **v1.3.x** — durable completion ordering (reply persist → summary →
  handoff → recall → rollover → terminal publication); the idle sentinel
  barrier; task state.
* **v1.5–v1.6** — engine variant provisioning through the authoritative
  resolver (no silent CPU fallback); clone + repo index; Lab workspaces;
  custom tools.
* **v1.7.0–v1.7.2** — reliability passes over the engine lifecycle, hub
  replay and stress surfaces; scheduler settlement contract.
* **v1.7.1** — the preflight gate (PREFLIGHT → REFUSE → NO ENGINE START)
  and the live monitor with hysteresis + synchronous cooperative
  critical protection; context-exhaustion recovery (typed conditions only,
  one bounded attempt); native engine as a first-class backend.
* **v1.7.4–v1.7.6** — pause/edit/resume; transactional edits with durable
  journals and ten-window fault-injection recovery; restart recovery
  claiming the checkpoint revision; token-aware codename gate;
  generation-aware spec cache.

Per-release narrative essays were consolidated; the tags, their tests and
`UPDATE.md` are the authoritative record.
