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

## Evidence labels used by the backlog

Findings below are labeled so that engineering work starts from what is
actually known, never from what a report asserted:

* **D — Documented/verified.** Supported by current repository behaviour,
  code, tests or CI. Can be acted on directly.
* **I — Architectural inference.** A reasonable implication of the current
  design, but NOT a measurement. Requires a measurement plan before
  implementation is trusted.
* **H — Hypothesis.** Requires profiling or an experiment before ANY
  implementation decision. Numbers attached to hypotheses (percentages,
  TPS, TTFT reductions, cache-hit rates, VRAM figures, "3x/5x/80%" style
  claims) are explicitly REJECTED as facts — they are not copied into this
  roadmap.

---

## v1.8 — Adaptive Runtime Intelligence

### v1.8.8 — hardening the shipped surface — **CURRENT RELEASE**

v1.8.8 makes no architectural bet and adds no new subsystem. It repairs
the v1.8.7 audit-job failure at its root, synchronizes every release
surface to one version, and removes the dependency-level security debt
(evidence in `changelog.md` §v1.8.8):

* **Codename-gate fixture repair** — the v1.8.7 JSON column-order
  determinism fixtures in `internal/tools/data_tool_test.go` used the
  retired product codename as a JSON key, so the tracked-tree scan
  failed the audit job and skipped both platform jobs. The fixtures use
  a codename-free key with identical ordering semantics; the gate stays
  strict and the regression stays real.
* **Release-metadata synchronization** — `package-lock.json` root
  metadata (stale at 1.8.5) is synchronized through the canonical
  version authority; every surface reads 1.8.8 from the one source.
* **Dependency security** — the high-severity advisory reported by
  `npm audit` is repaired through a compatible upgrade with the
  frontend dependency contract preserved and the full frontend suite
  re-run.
* **dataAnalysis hardening** — correctness gaps in the existing one
  data authority (parser edge cases, deterministic ordering with
  explicit tie-breakers, join/aggregate/quality/export semantics) are
  closed with regression coverage rather than new machinery.

### v1.8.7 — deterministic discovery + data-analysis authority

v1.8.7 repairs the v1.8.6 Windows CI failure at its root and upgrades
the one `dataAnalysis` tool into the deterministic data-analysis
authority (evidence in `changelog.md` §v1.8.7 and the suites listed
there):

* **Deterministic Tier-2 discovery** — a priority/level barrier retains
  candidates in (level, path) order and seals the scan at level drain;
  the winning candidate is a pure function of the dataset for any
  worker count (D — `internal/engdiscovery/discovery_barrier_test.go`).
* **Repeated-scan cache inversion removed** — deferred cache-parent
  seeds + deterministic cache persistence (D — same suite).
* **Avoidable scan work reduced** — toolchain-cache/temp noise dirs,
  no post-cap child enqueues (D — existing suites, honest measurement).
* **Data-analysis authority** — `analyze` / `aggregate` / `join` /
  `quality` / `export`, compact/table/json output modes, provenance
  metadata, deterministic JSON column order, orchestrator + toolset
  integration coverage (D — `internal/tools`, `internal/agent`,
  `internal/toolsets` suites).
* **Backend honesty** — pure-Go engine kept after the DuckDB
  evaluation; no SQL/Parquet surface; 256 MB bound stated, not
  hidden (D — documented in `ARCHITECTURE.md` §5 / `changelog.md`).

### v1.8.6 — Phase 2 of the staged engine program

v1.8.6 delivers Phase 2's execution-truth and resource-integration core
(evidence in `changelog.md` §v1.8.6 and the suites listed there):

* **Execution truth enforced end-to-end** — `GPU detected ≠ GPU available
  ≠ GPU selected ≠ GPU executed ≠ GPU verified`: enumeration selects but
  never verifies; verification requires the measured offload line or a
  still-valid `accelerator.ExecutionReceipt` (identity-checked — stale
  evidence can never verify a new engine); CPU stays verified by
  definition. (D — `internal/accelerator` suites)
* **The GPU transaction is authoritative, premature activation removed** —
  AUTO serving launches enable offload only on proven execution
  (per-boot offload line or valid persisted receipt); the bounded
  candidate transaction keeps its proving path (real generation +
  measured offload before commit; rollback truthful); manual GPU config
  and CPU-forced mode respected; offload evidence is per-boot. (D —
  `internal/llm` suites)
* **`/api/engine` and `/api/perf` agree** — one accessor, one evidence
  path, signature-validated memo (no stale poll-only snapshot can claim a
  current backend; poll order cannot move the stage backwards). (D —
  `internal/api` suites)
* **Resource integration through the ONE Governor** — the inference
  footprint (model file fact + planned KV) folds into the resource state
  and envelope; the run gate consults the Governor's measured envelope
  before any engine start; the engine process RSS is MEASURED and flows
  into the Governor; `/api/perf` serves the engine's real memory evidence
  with provenance (measured RSS / file facts / unknown KV named unknown).
  (D — governor/runtime/api suites)
* **Windows CPU telemetry through the existing seam** — GetSystemTimes
  delta with priming, shared state machine, Linux untouched, no second
  sampler. (D — `internal/governor/cpu_delta_test.go`)

Remaining Phase 2 scope (NOT implemented, listed for the next pass):

* The unified C++ execution path behind the ONE execution boundary
  (native engine still CPU-only; its capabilities are now explicit on
  every surface). (I)
* Measured prefill/cache/TTFT performance work — measure first, no
  speculative optimization. (I/H)
* A second GPU backend (SYCL/OpenVINO) is justified ONLY when the
  repository can provision, launch, test and verify it without a second
  provisioning/selection system. (I)

### v1.8.5 — Phase 1 of the staged engine program

Primary objective: SHEYTAN continuously understands its machine and adapts
runtime behavior while preserving host responsiveness.

Delivered in v1.8.0 (see `ARCHITECTURE.md` §Governor for the design and
`changelog.md` for the evidence):

* resource-state model — one coherent snapshot of measurable current reality
  (`internal/governor` `ResourceState`);
* Runtime Governor — the ONE runtime policy authority over the existing
  live-monitor telemetry (`internal/governor`, wired on the `Stack` poll
  path);
* CPU/RAM pressure handling — the shipped preflight pressure vocabulary
  (ok / warning / high_pressure / critical_pressure) reused as the
  evidence-backed GREEN/YELLOW/ORANGE/RED/EMERGENCY mapping, with a bounded
  rolling signal and an injected CPU seam where the platform can measure;
* pressure hysteresis — sample hysteresis in the monitor (unchanged
  ownership) plus time-dimension sustained-state tracking in the Governor;
* resource envelopes — admission, background, context-work and tool-
  concurrency posture with explicit adjustment classes (`live` /
  `next-run` / `reload`);
* workload/model admission control — resident-budget admission over
  available RAM, process RSS and a caller-supplied resident plan (mapped
  file size is never treated as resident); unknown evidence → conservative
  refusal;
* runtime self-inspection/self-model — provenance-carrying self-model
  (`SelfModel`) composed with the existing sysinfo and engine-metric
  authorities;
* system-health surface — `GET /api/governor` and the System Centre's
  Runtime Governor card (real values, unknowns named, no decorative
  telemetry);
* evidence/reasons — every envelope and admission decision carries
  deterministic, explainable reasons.

Hardening delivered across v1.8.x maintenance releases (details in
`changelog.md`): the dual-boundary streaming flush and live memory/self-model
surfaces (v1.8.2), and the session-list generation-guard extension to the
startup consumer with deterministic session-delete coverage at store, Go and
browser layers (v1.8.3). v1.8.4 hardened the same line with evidence-backed
repairs: the streaming flush became a triple-boundary, self-healing
scheduler (a lost MessageChannel delivery can no longer wedge visibility —
deterministic wedge-reproduction tests + a suspended-rAF browser E2E), the
activity flush lost its rAF-only dependency, zero-session Send became a
first-class contract (store-level + real-stack E2E), the context surface
gained a monotonic response-generation authority (causally ordered race
suite), the AUTO GPU posture became explicitly provenance-marked with a
one-time honest migration of the legacy derived state, and the engine
update's deferred-commit window now reports and probes the byte-verified
staged binary through a window-scoped identity marker.

### v1.8.5 — Phase 1 of the staged engine program

v1.8.5 completes PHASE 1 — the core runtime / user-surface foundation —
and leaves the deep execution-engine work explicitly to Phase 2:

* **Live streaming, server side closed (P0):** the run hub's subscriber
  delivery became a bounded, conflation-aware queue. The v1.2.x channel
  dropped the NEWEST event when a subscriber's buffer was full — for
  cumulative `response`/`reasoning` snapshots that policy discarded the
  frame carrying the FULL text while the buffer kept stale prefixes, so
  a backpressured/stalled transport froze visible text until the run
  ended (Stop included) and the reconnect replay revealed everything at
  once — the server-side twin of the v1.8.4 MessageChannel wedge. The
  queue evicts the OLDEST conflatable snapshot instead (newest-wins),
  preserves order and the seq replay contract, never preferentially
  drops terminal events, never blocks the publisher, and bounds memory;
  every event write now carries a generous deadline so a wedged client
  is torn down deterministically (deterministic hub/queue suites + race
  gate; the full API suite stays green).
* **Reasoning-depth ladder (P0):** the composer control is now
  Low / Mid / High / Ultra, and every level carries a REAL numeric
  thinking-token budget onto the generation request — the llama.cpp
  request-level `reasoning_budget_tokens` parameter, verified present
  in BOTH managed engine builds (b10642 default, b11205) by reading the
  actual server sources (low=0 thinking off, mid=1024 bounded default,
  high=4096, ultra=engine default/unrestricted). Local engines only
  (the same gating as the other llama.cpp-specific request fields);
  legacy Auto/Fast/Thinking values migrate at both the wire and the
  persisted-settings boundary. Not cosmetic, not prompt-faked.
* **Show/Hide Thinking (P0):** a visibility-only presentation control —
  the reasoning level and budget travel with every request regardless,
  the store keeps folding reasoning snapshots, and only the rendering
  of backend-reported reasoning is gated (persisted through the same
  settings authority; Chat and Agent share the semantics through the
  shared composer/message surfaces).
* **Engine is supervised machinery (P1):** the user-facing Start/Stop
  engine toggle is removed from the ordinary workflow; the engine boots
  on first use through the run gate, restarts after engine-affecting
  changes through the settings flow, and recovers through internal
  supervision. The UI represents engine state. All internal lifecycle
  operations (startup, update, restart, recovery, shutdown) are kept.
* **Execution/evidence ladder (Phase 2 foundation):** ONE shared
  structure (`internal/llm/execution.go`, surfaced as `/api/engine`'s
  `execution` block) composes the existing authorities into the monotone
  ladder `detected → backend-available → device-selected →
  model-loaded → generation-executed → execution-evidence → verified`.
  Device enumeration can never equal verified execution — the ladder
  stops at the first unproven rung and names it. No new sampler, no
  second policy engine: a pure composer over inputs from device
  detection, backend health, the accelerator selection memo, verified
  model loading, measured generation telemetry and runtime offload
  lines (deterministic ladder suites).

Phase 1 deliberately does NOT attempt the Phase 2 work below.

Exit condition: SHEYTAN can operate for long periods without unnecessarily
degrading the host and can explain measurable reasons for its runtime
decisions, with the critical-protection path (cooperative cancellation)
unchanged in its existing owner.

Scope boundary — v1.8 deliberately does NOT include the AI System builder,
computer-use, a full evaluation framework, multi-model routing or model
pools. Clean seams were created only where v1.8 needed them.

---

## PHASE 2 — Deep execution-engine / resource integration — **IN PROGRESS (core delivered in v1.8.6)**

Phase 1 (v1.8.5, above) built the runtime/user-surface foundation; v1.8.6
(the CURRENT RELEASE section above) delivered Phase 2's execution-truth /
resource-integration core with deterministic evidence. The remaining
scope below is NOT implemented by being listed here. Its priority order:

### Deep engine integration (HIGH)

* Finish the unified C++ execution path behind the ONE execution boundary
  (Go = control plane, C++/serving backend = execution plane; the v1.8.5
  `llm.ExecutionReport` ladder is the shared contract both planes speak). (I)
* Mature the llama.cpp/ggml integration where the repository's own evidence
  justifies it — model placement/splitting where the serving engine
  supports it, real memory/KV/resource accounting fed by the existing
  Governor seams, stronger engine/backend fallback and recovery. (I)
* Backend selection driven by PROVEN runtime evidence — consume the
  execution/evidence ladder's verified rung instead of detection-level
  posture wherever a selection decision is made. (I)
* The native C++ path has no reasoning-budget control today (the level is
  documented-inert there); wiring real native thinking budgets belongs to
  the native engine work, never a prompt-side fake. (I)

### Real GPU execution proof (HIGH)

* Physical/runtime offload evidence on supported hardware: correct
  device/backend selection, execution verification, truthful resource
  reporting — all keyed off the EXISTING stage → exact executable →
  probe → launch → health → generation → evidence → commit/rollback
  engine identity transaction. Never verify one binary and claim another. (D — the transaction rules exist; the physical validation is new)
* No hard-coded Intel/Arc claims; no Vulkan/SYCL/OpenVINO execution claim
  merely because a backend or device exists — the v1.8.5 ladder's
  detected ≠ verified rule is the contract. (D — invariant)

### Runtime performance foundations (MEDIUM/HIGH — only where measured)

* Prompt prefill measurement and cache/prefix reuse evidence (the
  existing I/H backlog items below roll up here). (I/H)
* TTFT and streaming wire efficiency — measured before any change; the
  v1.8.5 conflation queue is verified and is not to be redesigned absent
  a measured regression. (I)
* CPU telemetry on Windows through the EXISTING Governor/CPU sampler
  seam — no second sampler, no second authority. (I)
* Resource-aware generation/context budgets under the ONE Governor
  policy authority; RAM stays memory capacity, never an accelerator;
  unknown measurements stay unknown; no fake VRAM/offload numbers. (D — invariants; the budgeting itself is Phase 2 work)

### Remaining user-facing runtime polish (as it fits safely)

* Only items that cannot land in a Phase 1-style vertical slice without
  destabilizing the current surfaces — the Phase 1 disciplines (one
  authority per concern, no sleep-based correctness, no weakened tests)
  apply unchanged.

---

## PHASE 3+ — New product capabilities — **FUTURE**

The longer product ladder is unchanged and NOT implemented: the AI System
Builder (v1.9), Universal Agent (v1.10), Learning & Evaluation (v1.11),
Advanced Model Intelligence (v1.12) and the SHEYTAN Local AI Platform end
state (v2.0) — see the sections below. Nothing from these stages is pulled
into Phase 1 or Phase 2 unless an existing dependency explicitly requires
it.

---

## v1.8.x — Runtime hardening / performance foundations — **FUTURE UNLESS SHIPPED AND VERIFIED**

Everything in this section is engineering BACKLOG for the v1.8 line. None of
it is implemented by being listed here. Each item names its evidence class
(D/I/H per the labels above) and its non-negotiable constraints.

### Windows runtime telemetry — HIGH (D/I)

* Windows CPU load authority is still incomplete: the live CPU seam reports
  unknown on Windows and CPU policy stays off (documented limitation). (D)
* Add a measured `GetSystemTimes`, PDH or equivalent seam and feed the
  EXISTING Governor/CPU sampler — no second sampler, no second authority. (I)
* Preserve explicit unknowns when measurement fails; never fabricate a
  utilization number. (D — existing invariant)
* Validate under synthetic load before enabling CPU policy on Windows. (I)

### TTFT / prompt prefill / prefix reuse — HIGH (I/H)

* Measure prompt prefill and cache reuse BEFORE changing any architecture:
  expose prompt tokens and cached/reused tokens only when the engine
  provides real evidence for them. (I)
* Preserve stable prefix ordering where compatible:
  system → stable tool schemas → stable project/knowledge → volatile turn
  evidence. (I — matches the current context-plan stage order)
* Investigate session/slot affinity and KV reuse; chunked compaction only if
  it provably preserves memory semantics. (H)
* No cache-hit target is asserted — none has been measured.

### Streaming wire efficiency — HIGH (I)

* Incremental think-tag parsing instead of whole-buffer re-scans. (I)
* Measure browser first-paint of streamed text (the observable the v1.8.2
  dual-boundary flush protects) and WebSocket byte accounting. (I)
* Consider a delta + periodic-snapshot wire protocol with bounded
  backpressure, preserving reconnect/replay/idempotence and cumulative
  replace semantics. (I/H)
* The current dual-boundary flush solution is verified — it is not to be
  redesigned absent a measured regression.

### Small-model tool-call reliability — HIGH (D/I)

* Maintain the verified chat-template/model compatibility matrix as
  measured evidence, not assumption. (D — the matrix exists; keep it honest)
* Tolerant parsing for safe structured-text tool calls, with bounded
  recovery of malformed calls. (I)
* Schema-constrained decoding only where the real engine supports it. (I)
* Never execute ambiguous tool text; objective verification remains
  mandatory for Lab work. (D — existing invariant)

### Large tool-output handles — MEDIUM/HIGH (I/H)

* Spool oversized tool output to task-owned artifacts; inject a compact
  handle (metadata + head/tail preview); retrieve precise ranges/searches
  on demand. (I)
* Preserve security boundaries and cleanup ownership. (D — constraints)

### Safe read-only tool concurrency — MEDIUM/HIGH (I)

* Classify side effects; bound parallelism for independent read-only/
  idempotent work; keep mutating operations protected. (I)
* The Governor owns the concurrency envelope; deterministic result
  ordering for merged evidence. (I)
* Implementation stays in the existing Go runtime — no second async
  runtime is introduced for this.

### Coding Lab verification optimization — HIGH (I/H)

* Impacted-test selection and cheap/fail-fast checks where safe. (I)
* Trustworthy result caching keyed by workspace/input identity; bounded
  parallel independent checks. (H — needs identity semantics proven)
* Full objective verification always precedes promotion. (D — invariant)
* Track verification duration and repair iterations as measured evidence
  (feeds the v1.11 evaluation work).

### Repository index scaling — MEDIUM (I/H)

* Filesystem-event-driven incremental indexing plus a reconciliation scan;
  bounded parse workers; explicit partial-index state. (I)
* Better lookup structures only after profiling proves the need. (H)

### Session persistence scaling — MEDIUM/HIGH (I/H)

* Measure persistence latency and bytes written versus session size
  before any change. (I)
* Investigate append-only segments, snapshots or WAL — preserving atomic
  checkpoints/recovery. (H)
* No storage migration without crash/recovery tests. (D — constraint)

### I/O optimization — MEDIUM (H)

* Resumable/ranged downloads and incremental hashing; parallel provider
  requests under the existing timeouts; buffered logs only with explicit
  loss semantics. (H — the downloader remains the ONE download authority)

### Startup / cold-vs-warm readiness — MEDIUM (I)

* Resident-model policy under Governor budgets; avoid unnecessary engine
  reloads; optimize expensive probes. (I)
* The maintenance-before-engine-start guarantee is preserved. (D)

---

## v1.9 — AI System Builder — **FUTURE — NOT IMPLEMENTED**

The AI System as a first-class, user-owned object:

* durable user-owned AI-system identity;
* instructions/behavior policy;
* model policy (and model routing policy surfaces);
* tool/connector policy and permissions;
* memory, knowledge and skills policy;
* artifact-backed context and large-output handles;
* runtime envelopes / resource policy / performance profile;
* evaluation configuration;
* versioning, clone/export/import;
* a system-level definition-of-done.

Nothing in v1.8 pre-builds these surfaces; the Governor's policy seam is the
only shared foundation. Nothing here is implemented by being documented.

## v1.10 — Universal Agent — **FUTURE — NOT IMPLEMENTED**

Controlled computer-use layer, all through explicit permission/trust
boundaries: browser and desktop interaction, screen, mouse/keyboard,
application discovery, document workflows, multimodal execution,
connectors, voice foundation. Additionally:

* bounded parallel tool orchestration with a deterministic execution
  graph;
* tool-call state hashing for repeated-state/cycle detection — a candidate
  repetition guard hashes `tool name + normalized arguments + relevant
  state`; the guard is evidence-gated, and no fixed retry count is
  hard-coded until experiments establish that policy;
* bounded reflection/recovery and pause/cancel propagation through the
  run lifecycle;
* artifact/handle workflows (consume the v1.8.x large-output handles).

Never replace safety or objective verification with a circuit breaker
alone.

## v1.11 — AI Learning & Evaluation — **FUTURE — NOT IMPLEMENTED**

Evaluation suites, baselines, experiments, candidate variants, diagnostics,
feedback learning, skill evolution, memory optimization, routing/context
optimization, promotion/rollback, improvement history. Additionally:

* representative local-model benchmark/evaluation suites;
* tool-call reliability evaluation and the chat-template compatibility
  matrix as living evidence;
* prompt/context experiments; memory retrieval experiments; cache/prefix
  experiments;
* streaming performance regression tests; Lab verification-speed
  experiments;
* performance regression gates;
* candidate promotion/rollback decided ONLY by measured evidence.

Honesty gate: no system change becomes an "improvement" merely because the
model says so — only measured evaluation evidence promotes a candidate.
Every optimization needs a baseline and repeatable evidence.

## v1.12 — Advanced Model Intelligence — **FUTURE — NOT IMPLEMENTED**

Multi-model routing, capability discovery, specialist models,
vision/reasoning routing, draft/speculative models, dynamic loading and
unloading, model pools, cross-model fallback, resource-aware selection.
Additionally (all H until the actual serving engine supports them):

* native-engine optimization: multithreaded matvec, SIMD quantized
  kernels, batched prefill, blocked/optimized attention — each with
  numerical parity tests;
* KV/paged-cache research only where the actual serving engine supports
  it;
* optional local embedding/vector infrastructure only when RAG
  measurements justify it.

No specific external stack is mandatory for any of this.

## v2.0 — SHEYTAN Local AI Platform — **FUTURE**

Strategic end state: a complete user-owned local AI platform where users
create, run, understand, modify and continuously improve durable AI systems
while keeping ownership of their models, knowledge, memory, skills, tools,
behavior, experiments and versions; scalable local workers, mature
multi-run orchestration, cross-platform support, optional multi-process/
remote workers, mature observability/evaluation, portable import/export/
versioning.

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
  engine lifecycle, one memory authority, one runtime policy authority.
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
