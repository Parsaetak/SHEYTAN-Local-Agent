# SHEYTAN-LA™ (SHEYTAN Local Agent)

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.
> SHEYTAN measures the machine, governs the runtime and verifies the work.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around Go, React/TypeScript, Wails v3, managed llama.cpp inference, a native C++ engine path, controlled tools, isolated coding workspaces, Net Search, memory, recall, objective verification — and a Runtime Governor that continuously understands the machine and governs runtime policy while keeping the host responsive.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under a **conservative mixed model** — Apache-2.0 for explicitly designated open components, the **Parsaetak Proprietary License v1.1** for SHEYTAN-specific material. The complete licensing picture — model, classification, notices, and BOTH full legal texts — lives in `LICENSE.md`, the sole licensing artifact.

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Current release:  v1.8.2
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
frontend is a React/TypeScript surface embedded into the binary. A Runtime
Governor folds the existing live-pressure telemetry into a policy
authority — it owns policy only; every action stays with its existing
subsystem. Full truth: `ARCHITECTURE.md`.

## The live generation surface

While a run is active, exactly ONE assistant surface shows what is really
happening — the real lifecycle phase (Preparing → Thinking → Generating →
Finalising), the latest cumulative answer with a live cursor, the model's
own emitted reasoning when it streams one, bounded runtime activity
(tools, context, engine state), and a backend-truth memory line
(`Memory: session summary · 2 recalled exchanges`) that states exactly
what the memory authorities actually injected this turn — no fabricated
counts, no invented reasoning. Streamed text reaches that surface through
a dual-boundary flush (an event-loop task plus an animation frame) with
one coalescing latch: live text appears without any user action, updates
continuously, and the final message replaces it exactly once.

## Model self-knowledge

Ask "what tools do you have?", "what can you do?" or "what model are you
running?" and the model answers from a measured description of the actual
runtime — the live tool registry (with the concise description of each
offered tool, and disabled tools honestly named not-callable), the loaded
model's parsed card (architecture, quantization, parameters, context
limit, tokenizer family, chat-template support, vision only when real),
the serving backend and engine build, measured hardware with GPU/NPU
labeled detection-only, and which memory systems exist. The answer never
triggers web research or blind recall; the description is composed from
the same authorities the UI itself reads, so it cannot drift from what is
really enabled.

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
  protection (deterministic + race);
* the Runtime Governor policy loop with deterministic explanations
  (deterministic + race + API contract tests);
* the runtime self-model for capability questions, composed from the live
  registry and the parsed model card (deterministic + orchestrator-level
  E2E over a fake engine).

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

## Release history

Release history lives ONLY in `changelog.md`. This README describes the
current product; per-release evidence lives in the release tags, the tests
that shipped with them, and `UPDATE.md`.
