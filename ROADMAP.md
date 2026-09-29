# SHEYTAN-LA Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA after the
2026-09 R&D reset. It replaces the old v1.7.x-centered engineering log: the
release history lives in `UPDATE.md` and the architecture truth in
`ARCHITECTURE.md`. One rule governs the whole document: **future phases are
never marked complete because they are documented.** A capability is DONE
only when deterministic tests, race gates, integration or E2E evidence prove
it in this repository.

Motto: **"The model proposes. The tools execute. The laboratory verifies."**

---

## v1.8 — Adaptive Runtime Intelligence — **CURRENT RELEASE**

Primary objective: SHEYTAN continuously understands its machine and adapts
runtime behavior while preserving host responsiveness.

Delivered in v1.8.0 (see `ARCHITECTURE.md` §Governor for the design and
`UPDATE.md` for the evidence):

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

Exit condition (met at the scope boundary below): SHEYTAN can operate for
long periods without unnecessarily degrading the host and can explain
measurable reasons for its runtime decisions, with the critical-protection
path (cooperative cancellation) unchanged in its existing owner.

Scope boundary — v1.8 deliberately does NOT include the AI System builder,
computer-use, a full evaluation framework, multi-model routing or model
pools. Clean seams were created only where v1.8 needed them.

---

## v1.9 — AI System Builder — **FUTURE — NOT IMPLEMENTED**

The AI System as a first-class, user-owned object:

* identity, instructions/behavior, model policy, model routing;
* memory, knowledge, skills, tools, connectors, permissions;
* context policy, runtime policy, automations;
* evaluations, feedback, performance profile;
* versioning, clone/export/import.

Nothing in v1.8 pre-builds these surfaces; the Governor's policy seam is the
only shared foundation.

## v1.10 — Universal Agent — **FUTURE — NOT IMPLEMENTED**

Controlled computer-use layer, all through explicit permission/trust
boundaries: browser, desktop interaction, screen, mouse/keyboard,
application discovery, document workflows, multimodal execution,
connectors, voice foundation.

## v1.11 — AI Learning & Evaluation — **FUTURE — NOT IMPLEMENTED**

Evaluation suites, baselines, experiments, candidate variants, diagnostics,
feedback learning, skill evolution, memory optimization, routing/context
optimization, promotion/rollback, improvement history.

Honesty gate: no system change becomes an "improvement" merely because the
model says so — only measured evaluation evidence promotes a candidate.

## v1.12 — Advanced Model Intelligence — **FUTURE — NOT IMPLEMENTED**

Multi-model routing, capability discovery, specialist models,
vision/reasoning routing, draft/speculative models, dynamic loading and
unloading, model pools, cross-model fallback, resource-aware selection.

## v2.0 — SHEYTAN Local AI Platform — **FUTURE**

Strategic end state: a complete user-owned local AI platform where users can
create, run, understand, modify and continuously improve durable AI systems
while keeping ownership of their models, knowledge, memory, skills, tools,
behavior, experiments and versions.

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
