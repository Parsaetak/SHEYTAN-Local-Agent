# SHEYTAN-LA™ (SHEYTAN Local Agent)

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.
> SHEYTAN measures the machine, governs the runtime and verifies the work.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around Go, React/TypeScript, Wails v3, managed llama.cpp inference, a native C++ engine path, controlled tools, isolated coding workspaces, Net Search, memory, recall, objective verification — and, since v1.8.0, a Runtime Governor that continuously understands the machine and governs runtime policy while keeping the host responsive.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under a **conservative mixed model** — Apache-2.0 for explicitly designated open components, the **Parsaetak Proprietary License v1.1** for SHEYTAN-specific material. The complete licensing picture — model, classification, notices, and BOTH full legal texts — lives in `LICENSE.md`, the sole licensing artifact.

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Current release:  v1.8.1
Executable:       SHEYTAN-LA.exe
AppUserModelID:   Parsaetak.SHEYTAN-LA
Branch:           main
```

---

# What SHEYTAN is

SHEYTAN is NOT an Ollama/LM Studio wrapper. It is a laboratory: the model
proposes, the tools execute, and the laboratory verifies. The product is
Windows-first and local-first — computation, memory and knowledge stay on
the user's machine.

## Architecture in one paragraph

One Go process owns the runtime: a single engine lifecycle over managed
llama.cpp (with the native C++ engine as a validated alternative path), one
run lifecycle (fresh and resumed runs share one registry, one hub, one
authoritative state), one session store, one tool registry, one scheduler,
one downloader, one installer/update path and one memory authority. The
frontend is a React/TypeScript surface embedded into the binary. Since
v1.8.0 a Runtime Governor folds the existing live-pressure telemetry into a
policy authority — it owns policy only; every action stays with its
existing subsystem. Full truth: `ARCHITECTURE.md`.

## v1.8.1 highlights

* **P0 — the real-Windows local-generation crash is repaired at the root** —
  on a machine running a small local Gemma-class model, an ordinary `hi`
  turn panicked with a nil pointer dereference between `task classified`
  and `tier selected`, settling as an error in ~57 ms with no request ever
  reaching the engine. Root cause (reproduced deterministically): Gemma-class
  GGUFs carry ~262K-entry tokenizer blocks whose metadata exceeds the model
  card parser's old 8 MiB read bound, so the capability card resolved to nil
  and the orchestrator's estimator block dereferenced it. Two repairs: the
  documented nil-card fallback (configured context + conservative estimator)
  in `resolveEffectiveContext`, and a 32 MiB read bound so real Gemma-class
  cards parse again — restoring the model-aware context clamp and
  family-tuned token estimation for exactly that model class. Regressions
  pin both layers, including a full LOCAL-engine end-to-end `hi` run
  (engine request evidence, streamed response before `done`, one persisted
  reply, one settlement, a second ordinary chat).
* **P0 — streamed answers become visible through ONE render frame instead
  of two** — response/reasoning content previously waited for the activity
  batch frame AND the streaming flush frame before appearing. The new
  single-frame fast path folds stream-critical events into the streaming
  accumulator the moment the WebSocket delivers them; the timeline batch,
  sequence/stale-run protection, cumulative-snapshot semantics, replay
  idempotence and synchronous done/error/abort flushing are all preserved
  (12 deterministic frame-controller tests, no sleeps).
* **P1 — engine rollback now restores the recorded identity** — a failed
  startup verification after a package swap could leave the transient
  bundled-default tag stamped in `installed.json` while the restored
  manifest described the actually-serving engine; `Rollback()` now
  re-records from the restored manifest (regression-tested, including the
  user's observed b10642 transient shape).
* Audited with no change needed: the legacy AppData root migration
  (newer-wins collisions, one authoritative root, idempotent next boot) is
  the intended contract; GPU/AUTO CPU selection with `numGPU=0` remains the
  honest evidence-ladder outcome.

## v1.8.0 highlights (historical)

* **P0 — the Windows pause→resume→pause synchronization defect is repaired
  at the root** — the failing test (`TestPauseResumePauseAgainThenResumeCompletes`,
  Actions run `36553559366`) reused a wait helper whose condition is
  satisfied by the STALE pre-resume cumulative snapshot: the second pause
  was issued without proof the resumed generation was live, and on a slow
  runner the run settled `done` while the test waited for `paused`. The
  repair is an authoritative-evidence contract (`resumedGenerationEvidence`):
  after a resume, proof requires a strictly newer run sequence AND a changed
  cumulative response/reasoning snapshot. Deterministic regression coverage
  pins the contract so stale cumulative-state reuse cannot return.
* **P0 — aborted runs now carry an honest abort marker end-to-end** — a
  latent race discovered during the v1.8 audit: the orchestrator published
  `done` (with an abort caption) when a run context was canceled, so the
  live state flipped to `done` while the outcome registry recorded
  `aborted` — a divergence the one-settlement rule could never repair, and
  the source of a ~17% flake in the abort-after-resume integration test on
  Linux. The orchestrator now publishes the honest `aborted` activity type,
  the authoritative state folds it, both settlement paths capture the real
  terminal caption, and the frontend consumes the typed marker (no more
  caption-string sniffing). The flake is gone (15/15 stressed runs).
* **Runtime Governor (Adaptive Runtime Intelligence)** — one resource-state
  model over measurable facts (RAM, process/engine RSS, CPU where
  measurable, active runs); one pressure model with rolling signals and
  sustained-duration awareness on top of the shipped four-level vocabulary;
  one execution envelope (admit / reduce background / reduce context work /
  reduce tool concurrency) with honest adjustment classes (`live`,
  `next-run`, `reload`); resident-budget model admission (mapped file size
  is never resident size; unknown evidence → conservative refusal); a
  provenance-carrying self-model; deterministic explanations for every
  decision. Policy only — the Governor executes nothing.
* **System-health surface** — `GET /api/governor` composes the governor,
  hardware (existing sysinfo authority), engine health (existing metrics)
  and capability blocks; the System Centre renders the Runtime Governor
  card with real values, named unknowns and no decorative telemetry.
* **Docs/version truth** — documentation consolidated to v1.8.0 current +
  compressed history; release identity synchronized to 1.8.0 through the
  canonical gate (`package.json` → `config.AppVersion`, `build/config.yml`,
  `SIGNATURE`).

## Runtime Governor / self-model

The Governor (`internal/governor`) is fed by the EXISTING live-pressure
monitor — one sampler, one cadence, one protection path. It computes:

* **Resource state** — measurable current reality with explicit unknowns;
* **Envelope** — what may be admitted and what should be reduced, with the
  adjustment class that says how a recommendation can honestly be applied;
* **Admission** — heavyweight workloads and model loads are admitted
  against a resident budget (available RAM − host headroom − measured
  process RSS); unknown facts refuse conservatively;
* **Self-model** — verified facts with provenance (measured at, sustained
  for, what is unknown), served through `/api/governor` and rendered in the
  System Centre.

The critical-pressure protection path (cooperative cancellation of active
runs — never process killing) remains owned by the existing monitor wiring,
unchanged.

## Verified capabilities (evidence-backed)

The following are implemented and verified in this repository — each by the
evidence class named in parentheses:

* local llama.cpp inference with managed engines, the modern CLI contract
  (split `--cache-type-k`/`--cache-type-v`, `--flash-attn on|off|auto`) and
  strict numeric-argument validation before spawn (deterministic contract
  tests against binary `--help` fixtures);
* a native C++ engine path with real generation for validated
  llama-architecture models and honest fallback (unit + integration);
* pause / edit / resume with durable checkpoints, edit journaling, restart
  recovery and one authoritative run (deterministic + race + integration);
* sessions, memory, recall, skills, repository indexing, research, browser,
  sandbox, custom tools, coding lab, automations (unit/integration per
  surface);
* live resource monitoring with hysteresis and cooperative critical
  protection (v1.7.1, deterministic + race);
* the Runtime Governor policy loop with deterministic explanations
  (v1.8.0, deterministic + race + API contract tests).

Windows CI runs the full Go/frontend/native gates on GitHub-hosted runners;
Linux CI runs the headless gates. **Neither is a claim of physical-PC
runtime verification** — CI is CI.

## Installation

Windows-first. Download the release asset for your machine's variant,
run `SHEYTAN-LA.exe`. First run performs setup (data root, engine
provisioning through the authoritative variant resolver — no silent CPU
fallback for explicit variant requests). `SHEYTAN-LA.bat` offers a console
session; `SHEYTAN-LA.exe serve` runs the headless server. Models are managed
in-app (downloader is the ONE download authority); GGUF files can also be
imported from disk.

Build from source: Go 1.26, Node 24, `npm ci`, `npm run build`,
`go build` (Windows desktop; `-tags headless` for CI/containers/servers).

## Configuration

Live configuration is one concurrency-safe source (`config.Source`) shared
by every consumer — engine variant, model, sampling, thinking, tool policy,
net-search, update schedule, data root. The UI's Settings surface edits the
same source; `/api/config` is the machine surface. Context planning adapts
the window per session policy; the Runtime Governor's envelope is a policy
read-model, not a configuration file.

## Truthful limitations

* The Governor is a POLICY authority: it computes envelopes, admission
  decisions and explanations. Enforcement lives in the existing subsystems
  and is wired where control points exist; it is not yet a full closed-loop
  actuator over every background subsystem.
* CPU load is measured on Linux (normalized load average); on Windows the
  live CPU seam reports unknown and CPU policy stays off until a live
  Windows authority exists — never a fabricated utilization number.
* GPU/NPU facts on all surfaces are DETECTION-level; execution evidence is
  the engine's own device enumeration, verified at provisioning time.
* CI (Windows and Linux) is build/test evidence, not physical-host runtime
  evidence.
* The v1.9+ roadmap (AI System builder, computer-use, evaluation framework,
  advanced model routing) is NOT implemented — see `ROADMAP.md`.

## Release history (compact)

Every release through v1.7.6 was shipped with the same invariants: one
authority per concern, evidence over confidence, no sleep-based
correctness, honest hardware claims, the codename gate enabled.

* **v1.8.1 (current)** — real-Windows local-generation crash repaired at
  the root (nil capability card on Gemma-class GGUFs + the 8→32 MiB card
  read bound); single-frame streamed-answer visibility; engine rollback
  identity restoration; version truth 1.8.1.
* **v1.8.0** — pause/resume synchronization repair at the root;
  honest abort marker end-to-end (state/registry agreement restored); the
  Runtime Governor vertical slice (resource state, pressure model,
  envelope, admission, self-model) + `/api/governor` + System Centre card;
  documentation consolidated to current + history.
* **v1.7.6** — edit-transaction journals with ten-window crash/fault
  recovery matrix; restart-recovered runs claim the checkpoint revision;
  paused surface excludes journals; token-aware codename gate; generation-
  aware spec cache.
* **v1.7.5** — transactional pause/edit/resume (CAS validation, transcript
  replace, checkpoint commit, publication last); stale session-list
  generation guard; Windows migration test realism; numeric-variant gates.
* **v1.7.4** — pause/edit/resume state machine, durable checkpoints, WS
  replay continuity, double-pause idempotency, stop-after-pause semantics.
* **v1.7.3** — recovery/handoff durability and lifecycle hardening.
* **v1.7.2** — scheduler settlement contract, live-protection hardening.
* **v1.7.1** — preflight gate (PREFLIGHT → REFUSE → NO ENGINE START), live
  monitor with hysteresis + cooperative critical protection, native engine
  first-class backend, context-exhaustion recovery.
* **v1.7.0** — reliability pass: engine lifecycle, hub replay, stress
  surfaces.
* **v1.5–v1.6** — multi-agent context, memory/recall tiers, engine variant
  provisioning with authoritative resolver, Vulkan asset validation, clone
  + repo indexing, Lab workspaces.
* **v1.3–v1.4** — durable summaries, cross-mode history, task state,
  handoff, continuum rollover, repository index, custom tools, update
  rollback safety.
* **≤v1.2** — foundations: sessions, streaming, tool registry, downloader,
  installer, health checks, environment centre.

Full per-release narratives were consolidated; the authoritative record of
what each release changed is the release tag, the tests that shipped with
it, and `UPDATE.md`.
