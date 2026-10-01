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

## v1.8 — Adaptive Runtime Intelligence — **CURRENT RELEASE**

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
browser layers (v1.8.3).

Exit condition: SHEYTAN can operate for long periods without unnecessarily
degrading the host and can explain measurable reasons for its runtime
decisions, with the critical-protection path (cooperative cancellation)
unchanged in its existing owner.

Scope boundary — v1.8 deliberately does NOT include the AI System builder,
computer-use, a full evaluation framework, multi-model routing or model
pools. Clean seams were created only where v1.8 needed them.

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
