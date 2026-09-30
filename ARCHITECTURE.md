# SHEYTAN-Local-Agent — Architecture Truth Table

Current for **v1.8.2**. This document states the architecture that IS
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

**Model capability cards (v1.8.1):** the GGUF header parser reads up to
32 MiB of metadata (`ggufMetadataReadLimit`) — real Gemma-class tokenizer
blocks (262,144-entry arrays) exceed the old 8 MiB bound and made the card
unreadable. A card that still cannot be read resolves to a nil capability
object, and every consumer follows the documented fallback (configured
context, conservative estimator) — never a dereference. The capability
resolution itself is uniformly nil-config-safe.

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

**Streaming display (v1.8.2):** the backend emits cumulative
`response`/`reasoning` snapshots (SmoothStream, ~8 ms minimum emit
interval). In the frontend, stream-critical events fold into the streaming
accumulator AT SOCKET-RECEIVE TIME (`src/stream-fast-path.ts`). The flush
is scheduled on TWO deterministic boundaries (`src/stream-flush-scheduler.ts`)
— an event-loop task (MessageChannel) AND an animation frame; whichever
runs first flushes and the other no-ops — so visibility never depends on
compositor frame callbacks (the WebView2 rAF-throttling failure class:
text visible only after Stop). Exactly ONE coalescing latch remains; the
events still join the activity timeline batch, a self-draining ledger
guarantees the batch never processes them twice, and cumulative replace
semantics, replay/reconnect idempotence, sequence/stale-run protection and
the synchronous done/error/abort flush are unchanged.

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
