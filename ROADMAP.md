# SHEYTAN-LA Roadmap

## Purpose

SHEYTAN-LA is intended to become a serious local-first AI development environment: a desktop system that can understand a software project, reason about a task, inspect files and repositories, use tools and the internet, modify code, run software, test it, debug failures, verify outcomes, and deliver a finished result — without requiring a remote AI service for the core execution loop.

The target is not to reproduce another product's branding or implementation. The target is **functional parity with the core software-engineering workflow users expect from systems such as Codex and Antigravity, implemented around a local-first architecture**.

The v2.0.0.0 release is the point at which SHEYTAN-LA should be considered a complete product rather than a growing collection of strong subsystems.

---

# 0. ENGINEERING STATUS — read this first (v1.6.2, 2026-09-26)

This section is the AUTHORITATIVE forward handoff: the next AI agent
session can continue from this file alone. Read this section, then
`agent.md`, then the v1.6.2 sections of `ARCHITECTURE.md` /
`UPDATE.md` / `worklog.md`. Every item is explicitly marked
**COMPLETED / CURRENT / NEXT / FUTURE**, and every verification claim
names its evidence class (deterministic unit / race / CI-gate /
runtime / manual). Nothing planned is marked completed.

## COMPLETED

Only work actually VERIFIED in v1.6.2 (all evidence from this session's
fresh runs, on Linux amd64 unless noted):

* **COMPLETED (v1.6.2, reproduced then fixed, deterministic tests)** —
  P0 strict engine-variant parsing: `ParseAssetVariant`
  (internal/updater/variant.go) accepts exactly cpu/vulkan + documented
  aliases and rejects empty/unknown; `POST /api/engine/provision`
  answers a deterministic 400 for invalid variants. The v1.6.1 defect
  ({"variant":"banana"} silently provisioned CPU with a 200) was
  REPRODUCED by a test before the fix and is now a regression test
  (internal/api/engine_variant_strict_test.go). The lenient
  `NormalizeAssetVariant` survives ONLY for legacy manifest reads.
* **COMPLETED (v1.6.2, deterministic probe-injected tests)** — P0
  variant-aware release resolution: `ResolveDownloadURLForVariant`
  returns the pinned tag when its exact variant asset is verified, else
  the NEWEST release whose payload actually contains the exact variant
  asset (FirstWithVariantAsset). Tests pin: pinned-has-variant, pinned-
  lacks/newer-serves, newest-release-CPU-only-is-SKIPPED (the v1.6.1
  defect), no-matching-variant, invalid-variant, network-failure-
  preserved-verbatim, CPU backward compatibility. Current engine tag is
  preserved when it still serves the variant (no silent downgrade).
* **COMPLETED (v1.6.2, deterministic matrix tests)** — P0
  architecture-aware support matrix: windows/amd64 advertises
  cpu+vulkan; windows/arm64 does NOT advertise vulkan (verified
  upstream: b11191 has no llama-b11191-bin-win-vulkan-arm64 asset);
  linux/darwin stay cpu-only (upstream serves .tar.gz there — see
  CURRENT STATE limitations). Matrix and asset-naming are proven to
  agree for every GOOS/GOARCH combination tested.
* **COMPLETED (v1.6.2, CI contract rewritten)** — P0 CI: the workflow
  no longer hard-codes `$tag = "b10642"`. The Windows job runs
  `SHEYTAN_CI_VARIANT_GATE=1 go test ./internal/updater -run
  TestCIVariantAssetContract` — the SAME production resolver verifying
  exact variant asset resolution, exact asset reachability (HEAD
  2xx/3xx), matrix honesty, unsupported-variant refusal and no silent
  CPU fallback. Verified live on Linux (honest no-asset branch) in this
  session; the full branch runs on the Windows CI runner. The step
  states explicitly that HTTP reachability is NOT runtime execution.
* **COMPLETED (v1.6.2, reproduced then fixed, go test -race)** — P0
  GGUF import concurrency: exclusive per-models-directory lock
  (internal/llm/importlock.go: in-process mutex + cross-process
  flock/LockFileEx on `<models>/.import.lock`, kernel-released on
  process death). The race (two concurrent same-filename imports
  overwrote each other's completed file; same bytes produced 0
  duplicates) was REPRODUCED with -race before the fix. Now: different
  bytes → distinct collision-safe files; same bytes → exactly one
  duplicate result and one file; failed import → no .tmp residue;
  source never modified; refused (lock-held) imports place nothing.
* **COMPLETED (v1.6.2, deterministic tests incl. real fake-engine
  chain)** — P1 Vulkan runtime truth: the provisioning transaction is
  stop → staged install → startup/health → RUNTIME BACKEND
  VERIFICATION → commit. Verification evidence comes from the engine's
  own --list-devices enumeration (never filename/manifest/DLL
  presence): Vulkan devices enumerated (commit, evidence recorded),
  no Vulkan device (commit + honest note, CPU fallback serves),
  enumeration unsupported (commit + manifest-only attribution),
  enumeration cannot execute (ROLLBACK to last-known-good). Tests run
  the REAL chain: fake engine installed via the archive seam, started,
  health-checked, enumerated, committed.
* **COMPLETED (v1.6.2, deterministic tests, zero-spawn proofs)** — P1
  sampling contract audit closure: every launch argument emitted by
  buildArgsWithCaps and related paths audited. Raw user extra args
  (llamaExtraArgs) are now linted pre-spawn (config.ValidateExtraArgs):
  malformed numeric arguments (--temp abc, --top-k 1.5, --min-p=2)
  fail with ZERO engine spawns, never enter the compatibility ladder,
  never trigger accelerator downgrades; valid values and unknown flags
  pass through verbatim. maxTokens joins the deterministic contract.
  Repaired sampling values are PERSISTED atomically into the config
  file (only fields invalid IN the file; env-introduced corruption is
  never baked in; read-only/external configs degrade to in-memory
  repair with an honest note) — one repair, verified by a reload test.
* **COMPLETED (v1.6.2, frontend unit tests + typecheck)** — P1
  Settings surface: Settings → Performance → "Engine backend" card is
  REAL (GET/POST /api/engine/provision → updater transaction →
  restart → health/backend verification → UI state; 10-minute request
  timeout; honest evidence-gated hints). src/engine-backend.test.ts
  covers the state derivation (122/122 unit tests green incl. the 7
  new). ROADMAP/AI-CONTEXT no longer disagree.
* **COMPLETED (v1.6.2, documentation)** — stale llama.cpp/Linux
  statements corrected everywhere: upstream b11191 PUBLISHES Linux
  cpu+vulkan packages as .tar.gz (llama-b11191-bin-ubuntu-x64.tar.gz,
  llama-b11191-bin-ubuntu-vulkan-x64.tar.gz) — the zip-based installer
  does not consume them (the honest limitation, tracked in NEXT). The
  old "Linux binaries removed" claim is gone. Version metadata
  consistent at 1.6.2 (package.json → release-version.mjs → config.go
  / build/config.yml / SIGNATURE; `--check` green).

## CURRENT STATE

* **Current version:** v1.6.2 (single version hierarchy: package.json →
  release-version.mjs → config.go / build/config.yml / SIGNATURE;
  release-metadata consistency check green).
* **Verified architecture:** Go API + runtime (engine lifecycle with
  the maintenance gate, the sampling gate and the new extra-args gate;
  model-first selection state machine; transactional variant-aware
  engine provisioning WITH runtime backend verification), React/TS
  frontend (top-level CHAT | AGENT | WORKSPACE/LAB | SYSTEM | SETTINGS
  views; Settings → Performance now includes the Engine backend card),
  managed llama.cpp engine + native C++ engine backend contract,
  portable install-local data root with the migration chain,
  conservative mixed licensing (unchanged from v1.6.1).
* **Verified runtime state (this session, Linux amd64 host):** full Go
  test suites green (`go test ./...`, `go vet ./...`, `-race` on the
  import/variant/updater concurrency paths); frontend 122/122 unit +
  typecheck + lint + production build green. The reproduction-first
  evidence for every P0 fix is recorded in worklog.md (v1.6.2
  section).
* **Evidence classification (honest):**
  - Deterministic unit/race/CI-gate evidence: everything in COMPLETED
    above (runs on every CI pass).
  - Runtime evidence on Windows (real Vulkan hardware, real
    transaction, real backend enumeration): NOT executed by hand in
    this session — the session host is Linux without a GPU. The CI
    Windows job runs the resolver/asset gate; the runtime transaction
    is covered by the fake-engine chain tests. A hand-executed Windows
    Vulkan validation remains NEXT item 1.
  - Browser E2E was RE-RUN in this session with the real native C++
    engine: 24/24 passed (model-first, chat, agent settlement,
    sessions, composer attachments, lab). The native C++ engine suite
    passed 12/12 and the stress suite 47/47 (0 hangs, 0 crashes).
    Windows-runtime hand validation (real GPU Vulkan transaction) was
    NOT executed on this host (no GPU) — that alone stays NEXT item 1.
* **Known limitations:**
  - Linux engine download: upstream publishes .tar.gz packages; the
    zip-based transactional installer cannot consume them. Linux users
    self-build or use the native engine (documented in README).
  - Windows-only execution surfaces (Vulkan transaction against real
    hardware, comdlg32 picker, Job-Object process trees) are verified
    by deterministic tests + the CI matrix, not by hand this session.
  - The GGUF import's typed-path fallback is the only non-Windows
    input path (no GTK file-dialog integration).
  - AUTO does not auto-provision the Vulkan variant when evidence
    appears while a CPU package is installed — explicit policy, see
    NEXT item 4.
  - Only `internal/humanize/` is Apache-2.0-designated (conservative
    mixed licensing; LICENSE-MAP.md is the classification authority).

## NEXT (ordered, concrete, actionable)

1. **NEXT — Windows runtime validation of the v1.6.2 surfaces.** On a
   real Windows machine (or the Actions Windows runner with a GPU):
   run `POST /api/engine/provision {"variant":"vulkan"}` end-to-end,
   capture the engine's --list-devices Vulkan enumeration in the
   transaction outcome, verify `executionVerified=true` flows through
   `/api/perf`, exercise the Settings "Engine backend" card once, and
   record the evidence in worklog.md. Also re-run the browser E2E
   suite and record the fresh count (the v1.6.1 "24/24" number is
   stale and must not be quoted without a re-run).
2. **NEXT — tar.gz engine packages for Linux.** Upstream b11191
   publishes llama-<tag>-bin-ubuntu[-vulkan]-x64.tar.gz (and arm64).
   Extend the transactional installer's archive seam
   (InstallStagedFromArchiveDeferredWithVariant) to consume .tar.gz
   alongside .zip, then flip the Linux matrix to advertise the
   variants the assets actually serve (re-verify against the release
   list, same as the Windows Vulkan evidence). This unblocks Linux
   engine downloads AND Linux Vulkan provisioning.
3. **NEXT — import flow E2E + Settings guards.** Add a browser-E2E
   spec driving the "Import GGUF…" button (native picker stubbed
   through the typed-path fallback on Linux CI); mirror the
   repeat-penalty on-blur normalization for the other sampling fields
   in src/SettingsPanel.tsx.
4. **NEXT — AUTO variant-provisioning policy.** Decide + implement:
   when the accelerator resolution carries VERIFIED Vulkan evidence
   and the installed package is CPU on Windows x64, either (a) propose
   the swap through the maintenance gate with user consent, or (b)
   auto-provision through UpdateEngineVariantNow with rollback safety.
   Tests for both the decision and the transaction. Do NOT make AUTO
   claim Vulkan while a CPU package is installed.
5. **NEXT — deferred v1.6.0 items:** the AI-assisted custom-tool
   builder (manual builder is complete) and the unified Downloads
   Center page.
6. **NEXT — Stop/presence of a `--device` override UI** (small):
   expose the deterministic device selection the launcher already
   performs (llama.go deterministicDevice) in Settings → Performance
   next to the Engine backend card, read-only first.

## FUTURE (strategic, not yet started)

* **FUTURE — v1.8+: Internet + Repository Operations** (web search
  extraction depth, GitHub repository operations beyond cloning). See
  §6.
* **FUTURE — v1.9/v2.0 track: durable request/job queue, integrated
  IDE/engineering canvas, self-writing tool system, multi-agent
  architecture, system/runtime intelligence.** See §5, §9–§13 — these
  remain PLANNED (Part II discipline: nothing there is implemented).
* **FUTURE — broader Apache-2.0 designation.** Candidate components
  (only with maintainer sign-off and the LICENSE-MAP.md reclassification
  procedure): `internal/chunking`, `internal/histref` — currently
  proprietary by conservative default.
* **FUTURE — non-Windows native file pickers** (GTK/Zenity) for the
  import flow on Linux desktops.

The rest of this file is the strategic roadmap (v2.0 end state,
principles, capability pillars and the version plan) — its historical
goals remain authoritative and are deliberately preserved.

# 1. v2.0.0.0 End State

By `v2.0.0.0`, SHEYTAN-LA must be able to execute a complete software-engineering task locally from request to verified delivery.

The core loop is:

```text
USER REQUEST
    ↓
UNDERSTAND
    ↓
COMPILE TASK
    ↓
INSPECT PROJECT / SYSTEM / REPOSITORY
    ↓
BUILD CONTEXT
    ↓
PLAN
    ↓
EXECUTE
    ↓
OBSERVE
    ↓
TEST
    ↓
DIAGNOSE
    ↓
REPAIR
    ↓
VERIFY
    ↓
REVIEW
    ↓
PACKAGE / DELIVER
    ↓
PERSIST RESULT + EVIDENCE
```

A v2.0.0.0 installation must be able to perform this loop without requiring the user to manually orchestrate every step.

## v2.0 Definition of Done

SHEYTAN-LA is v2.0-complete only when all of the following are true:

* Chat Mode is a complete, polished local AI chat environment.
* Agent Mode is a complete autonomous engineering workflow with controlled execution.
* A durable request/job queue exists; user requests are not silently lost because another run is active.
* The application can inspect a local repository and, when permitted, inspect remote repositories and web resources.
* Repository understanding works across multi-file and large-project codebases.
* Context management can support models much smaller than the project being analyzed.
* The application can create and modify projects instead of only answering questions about them.
* The Coding Lab can build, test, debug, repair, and verify projects through repeatable workflows.
* An integrated IDE/canvas exists for editing, terminals, diagnostics, diffs, previews, tasks, and agent activity.
* System/runtime analysis can identify measurable bottlenecks and produce practical remediation steps.
* SHEYTAN-LA can create, test, version, register, and use new tools without manual code surgery.
* The application can recover from interrupted work, process crashes, model failures, tool failures, and partial execution without corrupting project state.
* Every consequential action is observable, cancellable, auditable, and verifiable.
* The system remains honest: unavailable capabilities are reported as unavailable rather than simulated.

---

# 2. Product Principles

## Local first

Core reasoning, project manipulation, execution, testing, verification, memory, and orchestration must work locally. Internet access is an optional controlled capability, not the foundation of the product.

## Evidence over confidence

The model does not decide whether its own work succeeded. Builds, tests, static analysis, runtime probes, assertions, diffs, and other objective evidence determine success.

## One authoritative state per concern

Run lifecycle, job state, engine state, model state, workspace state, tool state, and verification state each need one authoritative owner. Avoid duplicated state machines that can disagree.

## Small models must remain useful

The architecture should improve the effective capability of smaller local models through retrieval, structured context, task decomposition, deterministic tools, verification, memory, and specialized execution — not by pretending a small model has a larger context or capability than it actually has.

## Recoverability

Long tasks are expected. Restart, resume, retry, cancel, requeue, rollback, and continue must be first-class behaviors.

## Professional UX

Chat and agent workflows must feel like one coherent product. Diagnostics and engineering complexity should be visible when useful without forcing every user into a laboratory interface.

---

# 3. Capability Pillars

## Pillar A — Professional Chat Mode

Chat Mode must become a full local AI workspace rather than a simple prompt/response surface.

### Required capabilities

* Streaming responses with stable reconnect behavior.
* Conversation history and session persistence.
* Long-context conversation rollover without losing semantic continuity.
* File and directory attachments.
* Code-aware markdown and syntax rendering.
* Searchable conversation history.
* Conversation branching and regeneration where practical.
* Per-request model/config controls.
* Visible reasoning/tool activity without exposing internal chain-of-thought.
* Tool use when explicitly enabled and appropriate.
* Optional web/research access.
* Model capability indicators.
* Cancellation and retry.
* Export/import of conversations.
* Recovery after engine/process restart.
* Clear distinction between generated content and verified facts/results.

### Acceptance bar

A user should be able to conduct a long technical session entirely inside SHEYTAN-LA without the UI freezing, losing output, losing context, or requiring manual state repair.

---

# 4. Professional Agent Mode

Agent Mode becomes the primary engineering execution surface.

## Task lifecycle

Every task should have a durable `jobId` and an explicit lifecycle such as:

```text
ACCEPTED
QUEUED
WAITING_FOR_RESOURCES
RUNNING
WAITING_FOR_TOOL
WAITING_FOR_RETRY
VERIFYING
REVIEWING
COMPLETED
FAILED
CANCELED
PAUSED
RECOVERING
```

## Agent capabilities

* Inspect repository structure and configuration.
* Build a task-specific plan.
* Select required tools dynamically.
* Read only the context needed for the current step.
* Modify multiple files safely.
* Run commands in controlled environments.
* Run tests incrementally.
* Interpret failures.
* Repair failed changes.
* Re-run verification.
* Review the resulting diff.
* Summarize evidence and remaining limitations.
* Produce a final artifact/package when appropriate.

## Agent reliability

* Bounded retries.
* Failure classification before retry.
* Loop detection.
* Tool timeout and cancellation.
* Resource budgeting.
* Maximum execution budgets.
* Checkpointing.
* Crash recovery.
* Idempotent operations.
* Workspace snapshots.
* Safe rollback.
* Human approval gates for destructive or external actions.

---

# 5. Durable Request / Job System

The current one-active-run model must evolve into a real durable queue.

## v2 requirements

* Stable durable `jobId`.
* Persistent job storage.
* FIFO/fair scheduling.
* Per-session ordering.
* Bounded global concurrency.
* Resource-aware scheduling.
* Engine/model availability gating.
* Retry with bounded backoff.
* Priority where explicitly configured.
* Pause/resume.
* Cancel one job without destroying other queued work.
* Restart recovery.
* Crash recovery.
* Idempotent execution.
* Job history.
* Queue replay over API/WebSocket.
* Visible queue state in the UI.
* No silent request loss.
* No accidental duplicate execution.

## Architecture direction

Separate the concepts:

```text
User Request
    ↓
Durable Job
    ↓
Scheduler
    ↓
Execution Attempt
    ↓
Run
    ↓
Verification
    ↓
Job Settlement
```

A `Run` should no longer be the durable representation of user intent.

---

# 6. Internet, Repository and External-Research Layer

SHEYTAN-LA must be able to reason about the world outside the current working directory while maintaining strict trust boundaries.

## Required capabilities

* Search the public web.
* Fetch and analyze web pages.
* Read official documentation.
* Inspect public GitHub repositories.
* Inspect issues, pull requests, commits, and source files where permitted.
* Compare local code against upstream implementations.
* Read release notes and changelogs.
* Research dependencies and compatibility information.
* Collect evidence from multiple sources.
* Cite external evidence in the final result.
* Cache safe research results with freshness metadata.
* Work offline when internet access is unavailable.

## Repository intelligence

The agent should be able to ask:

```text
What changed?
Where is this implemented?
What depends on it?
What breaks if I change it?
What tests cover it?
What is the current architecture?
What does upstream do?
What is the smallest safe change?
```

## Trust boundary

External content is evidence, not authority.

Web pages, repositories, issue comments, README files, generated content, and downloaded files must never automatically become trusted instructions for local execution.

---

# 7. Context Engine for Small Models

This is one of the core differentiators of SHEYTAN-LA.

The goal is not simply to increase `n_ctx`. The goal is to make a small model behave as though it has a much larger working memory through intelligent external context management.

## Required layers

```text
Project Graph
    ↓
Structural Index
    ↓
Symbol / Dependency Index
    ↓
Semantic Retrieval
    ↓
Task Context Builder
    ↓
Context Budget
    ↓
Model
```

## Capabilities

* Repository map.
* Symbol index.
* Imports/dependencies.
* Call relationships where practical.
* File ownership/role classification.
* Configuration relationships.
* Test-to-source relationships.
* Error-to-code relationships.
* Git history awareness.
* Semantic search.
* Keyword/BM25 search.
* Hybrid retrieval.
* Hierarchical summarization.
* Persistent project memory.
* Session memory.
* Task memory.
* Verified facts separated from generated hypotheses.
* Context compression.
* Context rollover.
* Retrieval scoring based on task relevance.
* Duplicate-context suppression.
* Token/byte budgeting.
* Evidence-aware context escalation.

## Small-model strategy

When the model is weak or context-limited, improve the **environment around the model** rather than simply sending more text.

Examples:

```text
small model
+ strong repository map
+ targeted retrieval
+ structured plans
+ deterministic tools
+ independent verification
+ memory
+ specialized sub-agents
= higher effective engineering capability
```

---

# 8. Full Software Build / Test / Debug / Delivery Loop

SHEYTAN-LA must be able to take a project from an initial request to a verified build.

## Required workflows

### Build

* Detect project type.
* Detect build system.
* Detect dependencies.
* Detect environment requirements.
* Configure a clean build.
* Execute builds.
* Capture structured logs.
* Diagnose failures.

### Test

* Discover available test suites.
* Run targeted tests first.
* Expand to broader suites when necessary.
* Capture artifacts.
* Correlate failures with source changes.
* Detect flaky tests.
* Re-run only when evidence justifies it.

### Debug

* Reproduce failures.
* Capture environment state.
* Inspect logs and stack traces.
* Minimize the failing case.
* Identify likely root causes.
* Modify code.
* Reproduce again.
* Confirm the original failure is gone.
* Run regression tests.

### Finalization

* Review git diff.
* Check formatting.
* Check lint/static analysis.
* Check tests.
* Check packaging.
* Generate release notes or implementation summary.
* Produce evidence-backed completion report.

---

# 9. Integrated IDE / Engineering Canvas

SHEYTAN-LA needs a built-in engineering environment instead of making the user constantly switch between an AI panel, terminal, editor, and browser.

## Canvas components

### Editor

* Multi-file tabs.
* Syntax highlighting.
* Diagnostics.
* Search/replace.
* Symbol navigation.
* Go-to-definition/references where practical.
* Diff view.
* Inline agent changes.
* Safe apply/revert.
* File tree.

### Terminal

* Multiple terminals.
* Session persistence.
* Command history.
* Process visibility.
* Cancellation.
* Environment display.
* Structured command results.

### Agent panel

* Current task.
* Current plan.
* Current step.
* Active tool.
* Waiting state.
* Verification state.
* Queue position.
* Evidence.
* Diff summary.

### Preview/runtime panel

* Run the application.
* View local web apps.
* Show logs.
* Show process state.
* Capture screenshots.
* Restart/reload the target application.

### Git panel

* Status.
* Diff.
* Branch.
* History.
* Commit preparation.
* Safe revert.

### Research panel

* Search.
* Sources.
* Repository browsing.
* Documentation comparison.
* Evidence capture.

## Stability rule

The IDE must not become a second independent runtime. It should use the same authoritative Go runtime, job system, workspace system, tool registry, and verification infrastructure as Chat and Agent modes.

---

# 10. System and Runtime Intelligence

SHEYTAN-LA must understand the machine it is running on and use that knowledge to improve practical outcomes.

## Analyze

* CPU.
* Memory.
* GPU.
* NPU.
* Storage.
* Thermal/power state where safely available.
* Process load.
* Engine state.
* Model memory usage.
* Context size.
* Cache pressure.
* Build duration.
* Test duration.
* Tool latency.
* Network latency.
* Disk bottlenecks.
* Resource contention.

## Recommend

Recommendations must be evidence-based and actionable, for example:

```text
Detected:
  build is CPU-bound

Evidence:
  CPU saturation 91%
  GPU idle
  parallelism = 1

Recommendation:
  increase build parallelism to N
  because the build graph contains independent targets
```

Do not turn simple hardware detection into unsupported performance claims.

## Optimization assistant

The agent should be able to analyze an existing project and propose changes such as:

* Build caching.
* Parallelism.
* Compiler settings.
* Memory pressure reduction.
* Slow startup paths.
* Excessive network calls.
* Slow tests.
* Expensive allocations.
* Unnecessary context retrieval.
* Inefficient tool invocation.
* Poor process lifecycle management.

Where possible, recommendations should be verified by before/after measurement.

---

# 11. Self-Writing Tool System

SHEYTAN-LA must eventually be able to extend its own capability safely by creating new tools.

## Required behavior

The system should be able to:

1. Identify a missing capability.
2. Specify the tool contract.
3. Generate the implementation.
4. Generate tests.
5. Run the tool in a sandbox.
6. Verify behavior.
7. Register the tool only after validation.
8. Version the tool.
9. Roll back a broken tool.
10. Record provenance and evidence.

## Tool lifecycle

```text
PROPOSED
→ GENERATED
→ TESTING
→ VERIFIED
→ REGISTERED
→ AVAILABLE
→ DEPRECATED
→ REMOVED
```

## Tool safety

Generated tools must not automatically receive unrestricted filesystem, process, network, or secret access.

Capabilities must be explicit.

Generated tool metadata should include:

```text
name
version
purpose
input schema
output schema
permissions
network access
filesystem scope
process access
verification status
provenance
```

---

# 12. Multi-Agent Architecture

The current codebase already contains a sequential multi-agent foundation. v2.0 should turn the concept into a production execution model rather than a CLI-only experiment.

## Specialist roles

Use specialized roles where they materially improve reliability:

```text
Planner
Coder
Researcher
Debugger
Tester
Reviewer
Verifier
Documenter
Release/Packaging
Performance Analyst
```

Specialists should communicate through structured artifacts rather than unconstrained conversational state.

Examples:

```text
analysis.json
plan.json
patch.diff
findings.md
test-results.json
performance.json
verification.json
```

## Orchestration

The orchestrator must be able to decide when to:

* keep one agent working;
* ask a specialist;
* split independent work;
* merge results;
* reject a weak result;
* retry with different tactics;
* escalate to stronger context or a different model tier.

The architecture must avoid spawning unnecessary agents merely to appear sophisticated.

---

# 13. Model and Engine Strategy

SHEYTAN-LA should support a model-independent application layer while retaining local performance control.

## v2 objectives

* Multiple local model families.
* Capability-based model discovery.
* Model capability metadata.
* Context-window detection.
* Quantization awareness.
* Vision capability awareness.
* Tool-use capability awareness.
* Structured-output capability awareness.
* Model routing by task.
* Model routing by hardware budget.
* Model fallback.
* Model warm/cold lifecycle.
* Engine selection by measured evidence.

## Native engine

The native C++ engine should continue toward production readiness where it creates measurable value.

Priorities:

1. Correctness.
2. Compatibility.
3. Performance.
4. Accelerator support.
5. Packaging.
6. Broader model architecture support.

llama.cpp remains a strong backend rather than an implementation that must be replaced for ideological reasons.

---

# 14. Security and Trust Architecture

The more powerful the application becomes, the more important the containment model becomes.

## Required protections

* Loopback API by default.
* Strict WebSocket origin handling.
* Filesystem jail for project tools.
* Safe archive extraction.
* SSRF-resistant fetching.
* DNS/IP validation where appropriate.
* Explicit network permissions.
* Explicit process permissions.
* Secret redaction.
* No arbitrary destructive commands without policy approval.
* User confirmation for high-impact operations.
* Workspace snapshots before consequential changes.
* Rollback support.
* Untrusted external content treated as data.
* Tool provenance.
* Audit logs.
* Clear permission UI.

---

# 15. Observability and Evidence

Every important agent action should leave enough evidence to answer:

```text
What did SHEYTAN do?
Why did it do it?
What changed?
What command ran?
What failed?
What was retried?
What evidence says it succeeded?
What remains uncertain?
```

## v2 evidence model

A completed task should contain machine-readable evidence such as:

```text
job.json
plan.json
changes.json
commands.json
tests.json
verification.json
artifacts.json
```

The final UI should summarize this evidence in human-readable form.

---

# 16. Version Roadmap

The exact release count may change, but the capability order should remain stable.

## v1.2.8–v1.3 — Durable Work Foundation

Focus: make the current run-oriented architecture evolve safely toward durable work.

* Durable job model.
* Queue persistence.
* Job/run separation.
* Restart recovery.
* Per-session ordering.
* Queue UI.
* Job replay.
* Cancellation and pause semantics.
* Job checkpoints.
* Improved task history.

**Exit condition:** a user can submit multiple tasks without losing work because another task is running.

---

## v1.4 — Repository Intelligence

Focus: understand real projects deeply.

* Repository indexing.
* Symbol/dependency graph.
* Hybrid search.
* Project summaries.
* Git-aware context.
* Test/source relationship mapping.
* Error/source relationship mapping.
* Incremental index updates.
* Context relevance scoring.

**Exit condition:** the agent can locate the right parts of a large repository without dumping the repository into the model context.

---

## v1.5 — Small-Model Context Engine

Focus: maximize effective capability of smaller local models.

* Hierarchical retrieval.
* Persistent project memory.
* Task memory.
* Verified-fact store.
* Context compression.
* Context rollover.
* Retrieval planning.
* Tool-result compression.
* Context budget optimizer.
* Model-aware context strategies.

**Exit condition:** large repositories remain usable even with relatively small local models.

---

## v1.6.1 — SHIPPED (2026-09-26): P0 Sampling Gate, GGUF Import, Log Discipline, Real Vulkan, Mixed Licensing

The shipped v1.6.1 was a correctness/capability/governance release driven
by the in-the-wild `--repeat-penalty 0` engine failure (the value fed the
compatibility ladder and produced a retry storm of engine spawns).
Shipped and tested (full verification matrix in `worklog.md` §v1.6.1):

* Deterministic sampling gate — one validation authority
  (`internal/config/sampling.go`) applied at config load (safe repair,
  reported), env overrides, the Settings PATCH API (actionable 400), the
  engine boot gate (BEFORE any engine process / download / ladder;
  classified `InvalidSamplingConfigError` — a configuration failure,
  never an accelerator/compatibility verdict) and the launch arguments
  (numeric range rules). Invalid values never spawn the engine (real
  spawn-count evidence: 4 pre-fix → 0 post-fix).
* First-class local GGUF import — header validation before any bytes
  move, streaming 1 MiB copy, atomic placement, duplicate-safe naming,
  source never modified; API + native Windows picker + picker UI;
  retired-root model-path re-anchoring. Verified with the real in-repo
  GGUF fixture.
* Log discipline — accelerator resolutions/enumeration warnings log at
  INFO only on change; updater stale-file reporting aggregated; secrets
  never logged (audited).
* Real Windows Vulkan provisioning — backend-variant engine packages
  (manifest `variant: cpu|vulkan`), the real upstream Vulkan asset
  through the SAME transactional installer, engine-owned
  `UpdateEngineVariantNow` + `POST /api/engine/provision`; explicit
  VULKAN never silently falls back to CPU; AUTO stays evidence-gated; a
  Windows CI gate HEAD-checks the pinned Vulkan asset.
* Conservative mixed licensing + governance files (LICENSE-APACHE,
  LICENSE-PROPRIETARY, LICENSE-MAP.md as the classification authority,
  NOTICE.md, CONTRIBUTING.md, SECURITY.md; SPDX headers on the
  Apache-designated component).

Deferred honestly: the AUTO variant-provisioning policy (NEXT item 4 in
§0), the engine-variant Settings surface (NEXT item 6), and the import
button browser-E2E spec (NEXT item 3).

---

## v1.6.0 — SHIPPED (2026-09-26): Startup Integrity, Top-Level Views, Custom Tools

The shipped v1.6.0 re-prioritized the release around runtime integrity
and honest UX (the v1.5.1 startup ordering defect and the v1.6 failure
post-mortems drove this). Shipped and tested:

* Startup maintenance gate — engine maintenance completes BEFORE any
  engine start; prewarm and the scheduled updater's first pass are
  released together after the decision (explicit synchronization, no
  timing assumptions). Last-known-good engine survives failed updates;
  an unusable engine blocks startup honestly.
* Truthful compatibility diagnostics — every compatibility fallback
  records a concrete reason; genuinely unknown reasons say `unknown`.
* Chat and Agent as real top-level views (CHAT | AGENT | WORKSPACE/LAB |
  SYSTEM | SETTINGS); the internal segmented selector is gone.
  Independent per-mode histories, shared memory/tools/engine preserved.
  Repair pass: the navigation exposes real tab semantics
  (`tablist`/`tab`/`aria-selected`), a fresh install lands on Chat
  through one deterministic resolution (hash > remembered view > Chat)
  with the mode bound at first paint, and every view owns an explicit
  `#hash`.
* Automatic long context — the user-facing context-window control is
  removed; the physical window is managed automatically; logical
  conversation continuity is unbounded through history, summaries,
  retrieval, chunking, rollover and memory.
* Custom tools — first-class tools in the ONE registry, created from
  Settings → Agent & Tools → My Tools, validated/persisted/executed
  end-to-end with explicit permissions, bounded timeouts and output.
  Repair pass: local commands run under per-invocation process-tree
  ownership (Windows Job Objects — cancellation terminates the
  complete descendant tree and the inherited pipes close promptly);
  the output-cap test reads its large payload from a file instead of
  argv.
* Engine maintenance UX matches the real lifecycle (truthful phase
  sequence via /api/maintenance + the maintenance banner).

Deferred honestly: the AI-assisted tool builder (manual builder is
complete) and a single unified Downloads Center page (per-surface
truthful progress shipped instead).

---

## v1.7 — Full Agent Engineering Loop (moved forward from the old v1.6 slot)

Focus: make the Coding Lab a production execution system.

* Reproduction workflows.
* Automated build/test discovery.
* Failure classification.
* Repair plans.
* Incremental test execution.
* Regression detection.
* Patch review.
* Safe promotion.
* Artifact production.
* Verification reports.

**Exit condition:** SHEYTAN can take a real coding task from issue/request through implementation and verified completion without manual orchestration.

---

## v1.8 — Internet + Repository Operations

Focus: make external knowledge a first-class engineering input.

* Web search.
* Web page extraction.
* GitHub repository inspection.
* Commit/PR/issue analysis.
* Documentation comparison.
* Source provenance.
* Research memory.
* Evidence/citation UI.
* Offline fallback behavior.

**Exit condition:** the agent can research an unfamiliar technology and inspect a remote codebase as part of the same engineering task.

---

## v1.9 — Integrated IDE / Engineering Canvas

Focus: replace fragmented workflows with one application surface.

* Professional editor.
* Terminal workspace.
* File tree.
* Git panel.
* Diff/review UI.
* Live application preview.
* Agent timeline.
* Queue/task board.
* Research panel.
* System/performance panel.
* Workspace snapshots.

**Exit condition:** a user can reasonably build and debug a project without leaving SHEYTAN-LA for the normal engineering workflow.

---

## v1.10 — Self-Extending Tooling + Performance Intelligence

Focus: make SHEYTAN able to extend and optimize itself safely.

* Tool SDK.
* Generated-tool lifecycle.
* Tool sandboxing.
* Tool testing and registration.
* Capability discovery.
* System profiler.
* Project profiler.
* Build/test performance analysis.
* Practical optimization guidance.
* Before/after measurement.
* Performance regression detection.

**Exit condition:** SHEYTAN can create a missing tool, validate it, and use it; it can also diagnose measurable performance problems in a project or runtime.

---

## v2.0.0.0 — SHEYTAN-LA Complete Local Engineering Environment

This is the completion milestone.

### Required product capabilities

* Fully professional Chat Mode.
* Fully professional Agent Mode.
* Durable queue/job execution.
* Local repository intelligence.
* Internet/research access with trust boundaries.
* Large-project context management for small models.
* Full build/test/debug/fix/verify workflows.
* Integrated IDE/canvas.
* System and application performance analysis.
* Self-generated validated tools.
* Multi-agent specialist orchestration.
* Local model routing.
* Engine fallback/recovery.
* Persistent memory and project intelligence.
* Crash recovery and resumable jobs.
* Evidence-backed task completion.
* Production packaging and upgrade path.

### v2.0 user-level acceptance test

A user should be able to open SHEYTAN-LA and say:

> "Inspect this repository, understand the architecture, research the relevant documentation, reproduce the bug, implement the fix, run the tests, repair any failures, check the resulting performance, review the diff, and give me the finished project."

SHEYTAN-LA should be able to perform that task locally, using its own workspace, tools, models, context system, queue, IDE, research layer, and verification system, while exposing the work and evidence clearly to the user.

The system does not need to match a cloud system's speed. It needs to provide the **same class of engineering capability locally, reliably and transparently**.

---

# 17. Release Gates for Every Major Milestone

No milestone is complete because a feature exists in code or in the UI.

Each major milestone must pass:

```text
INSPECT
→ REPRODUCE
→ ROOT CAUSE
→ PLAN
→ IMPLEMENT
→ VERIFY
→ REGRESSION CHECK
→ STRESS
→ CLEAN-ROOM BUILD
→ PACKAGE
→ VERIFY PACKAGE
→ FINAL FACTUAL REPORT
```

## Mandatory verification categories

### Functional

Real user workflows execute successfully.

### Reliability

Cancellation, restart, reconnect, timeout, and failure recovery behave correctly.

### Security

New capabilities do not bypass existing sandbox or trust boundaries.

### Performance

Measured behavior is recorded; no unsupported benchmark claims.

### UX

Chat, Agent, IDE, queue, diagnostics, and recovery paths remain usable under real workloads.

### Packaging

Release artifacts come from the final verified source tree and are independently checked.

### Documentation

Documentation describes actual shipped behavior and clearly labels future capabilities.

---

# 18. What v2.0 Must Never Become

SHEYTAN-LA should not become:

* a cloud wrapper pretending to be local;
* a collection of disconnected AI demos;
* a UI shell around an external application;
* a system that claims GPU/NPU support without execution evidence;
* an agent that silently loses user work;
* an autonomous process with unrestricted destructive access;
* an IDE with a separate, conflicting runtime architecture;
* a multi-agent framework where extra agents exist only for complexity;
* a context system that dumps ever-larger prompts into small models;
* a product that reports success because the model said it succeeded.

---

# 19. Final Direction

The long-term product model is:

```text
SHEYTAN-LA
│
├── Chat
│   └── professional local AI workspace
│
├── Agent
│   └── durable engineering jobs
│
├── IDE / Canvas
│   └── editor + terminal + diff + preview + Git
│
├── Context Engine
│   └── project intelligence + retrieval + memory
│
├── Research
│   └── web + repositories + documentation
│
├── Tool System
│   └── built-in + generated + validated tools
│
├── Coding Lab
│   └── build + test + debug + repair + verify
│
├── Runtime Intelligence
│   └── hardware + process + project + performance
│
├── Model / Engine Layer
│   └── local models + routing + recovery
│
└── Evidence System
    └── facts + actions + tests + artifacts + verification
```

The central objective is simple:

> **Make SHEYTAN-LA capable of doing serious software engineering locally from beginning to end, with smaller local models becoming more capable through architecture, tools, context, memory, orchestration and verification.**

That is the bar for `v2.0.0.0`.
