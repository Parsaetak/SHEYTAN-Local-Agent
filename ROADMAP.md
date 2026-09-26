# SHEYTAN-LA Roadmap

## Purpose

SHEYTAN-LA is intended to become a serious local-first AI development environment: a desktop system that can understand a software project, reason about a task, inspect files and repositories, use tools and the internet, modify code, run software, test it, debug failures, verify outcomes, and deliver a finished result — without requiring a remote AI service for the core execution loop.

The target is not to reproduce another product's branding or implementation. The target is **functional parity with the core software-engineering workflow users expect from systems such as Codex and Antigravity, implemented around a local-first architecture**.

The v2.0.0.0 release is the point at which SHEYTAN-LA should be considered a complete product rather than a growing collection of strong subsystems.

---

# 0. ENGINEERING STATUS — read this first (v1.7.0, 2026-09-26)

This section is the AUTHORITATIVE forward handoff: the next AI agent
session can continue from this file alone. Read this section, then
`agent.md`, then the v1.7.0 sections of `ARCHITECTURE.md` /
`UPDATE.md` / `worklog.md`. Every item is explicitly marked
**COMPLETED / CURRENT / NEXT / FUTURE**, and every verification claim
names its evidence class (deterministic unit / race / E2E / CI-gate /
runtime). Nothing planned is marked completed. Stale v1.6.2 claims
(test counts, runtimes, hardware statements) are NOT inherited — the
evidence below comes from fresh v1.7.0 runs only.

## COMPLETED

Only work actually VERIFIED in v1.7.0 (all evidence from this
session's fresh runs, Linux amd64 build host):

* **COMPLETED (v1.7.0, deterministic rollback-instant probe tests + full
  package suite)** — P0 Windows transactional rollback hardening. The
  verification-failure path of `UpdateEngineVariantNow` (and every
  equivalent transactional engine-update path) now STOPS AND REAPS the
  candidate engine BEFORE any rollback filesystem mutation:
  `stopCandidateForRollback` (internal/llm/llama.go) uses the
  lifecycle-owned Stop (SIGTERM → bounded grace → Kill → deterministic
  reap on exitDone), cancels any armed watchdog, and resets the
  deliberate-stop marker. Regression suite
  (internal/llm/variant_rollback_v170_test.go) proves at the EXACT
  rollback instant: candidate process stopped and reaped, engine port
  free, previous package byte-identical (tree hash), previous
  manifest/variant authoritative, last-known-good restarts healthy,
  no orphaned process after the final stop, and the commit path never
  enters the rollback seam. Rollback failures now carry explicit
  restart evidence, and `StateFailed` is set only when nothing serves.
  The full internal/llm suite passed fresh (the v1.6.2 Windows CI
  failure class — "rename ...bin.update-old ...bin: Access is denied"
  under a live candidate — is structurally eliminated on every
  platform; no sleeps, no taskkill, no timing hacks).
* **COMPLETED (v1.7.0, deterministic unit + race + API tests)** — P1
  chronological task scheduling + automation. The ONE scheduler
  (internal/scheduler) gained: the v1.7.0 task model (enable/pause,
  once / interval / daily / weekly at LOCAL time, linked skills,
  task-scoped tools, task types, last-run pointer, run history,
  next-due), RunNow (async, bounded, deterministic refusal),
  Pause/Resume (resume recomputes NextDue — no stale burst),
  CancelRun, UpdateTask (revalidated, timeline reset on schedule
  change), Runs, NotifyEvent and ShutdownSettle. The v1.2.9
  durable-claim guarantee is extended to EVERY schedule kind: claims
  persist before execution; a crash cannot replay a claimed deadline;
  concurrent runs of one task are rejected; missed deadlines run
  exactly once with the next occurrence strictly after the claim.
  Covered by internal/scheduler/automation_test.go (incl. claim
  persisted while a run is IN FLIGHT) and the real HTTP paths in
  internal/api/automation_api_test.go (lifecycle, validation,
  deterministic errors, chronological run history).
* **COMPLETED (v1.7.0, deterministic tests)** — P1 Markdown SKILL.md
  packages with progressive disclosure (internal/skills/markdown.go):
  frontmatter + body parsing/validation (path-safe ids, bounded size,
  no reference escapes), metadata-first discovery, bodies loaded only
  for matching tasks, declared references only when required, scopes
  global/workspace/task with task > workspace > global precedence,
  malformed packages skipped honestly. Convergence into the SAME Skill
  authority via ToSkill (the JSON path is untouched and regression-
  green). Agent-facing skill_create is validated, task-scoped, and
  refuses global creation; PromoteTaskSkill enforces the existing
  VERIFIED-learning rule (claimed/partial evidence refused).
* **COMPLETED (v1.7.0, deterministic tests + real agent-loop E2E)** —
  P1 task-scoped agent-created tools
  (internal/customtools/tasktools.go): TaskDefinition + TaskStore under
  the existing custom-tools root, full lifecycle CREATE → VALIDATE →
  OPTIONAL APPROVAL → REGISTER → EXECUTE → CAPTURE → CLEAN UP, disabled
  AND unapproved by default, executed through the ONE orchestrator
  registry with the SAME schema validation, executors, permission
  controls, timeouts, output limits and secret-redaction rules. E2E
  (internal/agent/tasktool_e2e_test.go): approved task tool offered →
  executed → result in the follow-up turn → unregistered at teardown;
  unapproved tools never offered.
* **COMPLETED (v1.7.0, deterministic tests)** — P1 first-class
  task/run artifacts (internal/artifacts/taskmeta.go): durable
  TaskRegistry (registry.jsonl) with task/run/source/type/path/size/
  version/hash provenance; atomic, path-safe, bounded (8 MiB)
  creation; per-path version history where the superseded file is
  archived so EVERY version stays readable; restart-safe sequence
  numbers; task teardown cleanup. The agent-facing artifact_create
  operation is task-scoped (refuses outside a task run), and the API
  serves list/create/content/versions with a deny-by-default sandbox
  CSP (HTML served as inert source; SVG only through <img>; no
  artifact JavaScript in the application origin).
* **COMPLETED (v1.7.0, real-loop integration E2E)** — P1 integration of
  schedules + skills + tools + artifacts
  (internal/runtime/automation.go + automation_test.go): the runtime's
  taskRunner installs the task context, task-scoped tools and the
  bounded linked/task-skill block for the run, then the REAL agent loop
  calls artifact_create and the artifact lands in the durable registry
  with full provenance; the run output echoes the registered artifact
  count. Genuine event emitters wired where subsystems already know
  the event: boot = startup, real tool file-writes = file_change,
  succeeded clone = git_change, failed Lab verification = test_failure
  (hooks at the ONE settlement/creation points — no synthetic events).
  UI: the Automation surface (src/AutomationPanel.tsx +
  AutomationArtifactViewer.tsx) renders the chronological timeline
  FROM PERSISTED SCHEDULER STATE with Run now / Pause / Resume /
  Cancel / Delete, next-run, status, duration, trigger, linked
  skills/tools, artifact viewer (Markdown first-class), scoped-tool
  approval gate; frontend typecheck + unit tests green.

## CURRENT STATE

* **Current version:** v1.7.0 (single version hierarchy: package.json →
  release-version.mjs → config.go / build/config.yml / SIGNATURE;
  release-metadata consistency check green).
* **Verified architecture:** everything v1.6.2 shipped (engine
  lifecycle with maintenance/sampling/extra-args gates, model-first
  selection state machine, transactional variant-aware provisioning
  with runtime backend verification and NOW hardened rollback) plus the
  v1.7.0 automation layer (scheduler + SKILL.md + task tools + task
  artifacts + event emitters + /api/automation surface + Automation
  UI).
* **Fresh verification evidence (this session, Linux amd64 host):**
  full `internal/llm` suite green; `internal/scheduler`,
  `internal/skills`, `internal/artifacts`, `internal/customtools`
  green (scheduler additionally under `-race`); automation API tests
  green over real HTTP; agent task-tool loop E2E green; runtime
  integration E2E green; frontend `npm run typecheck` + unit tests +
  lint green. `go build -tags headless ./...` green. The FULL
  `go test -tags headless ./internal/...` sweep and the remaining
  regression battery are recorded in the packaging section of
  worklog.md — consult worklog.md for the exact fresh numbers; no
  count from v1.6.2 is quoted as current.
* **Windows/Linux differences (honest):** the Windows rollback failure
  class is fixed by construction and regression-guarded on Linux via
  the rollback-instant probe; Windows still locks running executables
  (that is WHY the fix exists) — the Windows CI job must re-run green
  on the Actions runner, and a hand-executed Windows Vulkan
  transaction remains untested on this host (no GPU, no Windows).
* **Known limitations:**
  - Linux engine download still blocked by upstream .tar.gz packaging
    (zip-only installer; documented since v1.6.2).
  - ci_failure / build_failure triggers remain DECLARED kinds with no
    genuine emitters yet (no synthetic events were invented) — see
    NEXT.
  - Task-run reports carry provenance through the output echo +
    artifact registry (per-run artifact grouping in the UI timeline
    uses the registry's runId).
  - AUTO still does not auto-provision the Vulkan variant (unchanged
    v1.6.2 policy, see NEXT).
  - Only internal/humanize/ is Apache-2.0-designated (conservative
    mixed licensing; LICENSE-MAP.md is the classification authority).

## NEXT (ordered, concrete, actionable)

1. **NEXT — Windows CI re-validation of the v1.7.0 rollback fix.** Push
   v1.7.0, confirm Actions run green on windows-latest
   (go test ./internal/... incl. TestVariantTransactionRollsBack...,
   TestVariantRollbackStopsCandidateBeforeRestore), then hand-run one
   real Vulkan provisioning on a Windows machine with a GPU and record
   the evidence in worklog.md.
2. **NEXT — genuine ci_failure / build_failure emitters.** The
   scheduler's event taxonomy is ready; wire the existing subsystems
   that genuinely observe those conditions (native C++ build results,
   pipeline/codeExec verification outcomes) to NotifyEvent, with tests
   asserting only real events fire.
3. **NEXT — per-run artifact grouping in the timeline.** Add RunID to
   scheduler.Report at execute() time (persisted) so the UI can group
   artifacts strictly under their run entries end to end.
4. **NEXT — tar.gz engine packages for Linux** (carried from v1.6.2,
   still blocked): extend the installer's archive seam to .tar.gz and
   re-verify the matrix against the real release list.
5. **NEXT — AUTO variant-provisioning policy** (carried from v1.6.2):
   decide propose-with-consent vs auto-provision; never claim Vulkan
   while a CPU package is installed.
6. **NEXT — deferred v1.6.x items** (carried): AI-assisted custom-tool
   builder, unified Downloads Center, --device override UI in
   Settings → Performance, import-flow browser E2E.

## FUTURE (strategic, not yet started)

* **FUTURE — v1.8+: Internet + Repository Operations** (web search
  extraction depth, GitHub repository operations beyond cloning). See
  §6.
* **FUTURE — v1.9/v2.0 track: durable request/job queue, integrated
  IDE/engineering canvas, self-writing tool system, multi-agent
  architecture, system/runtime intelligence.** See §5, §9–§13 — these
  remain PLANNED (Part II discipline: nothing there is implemented).
  The v1.7.0 automation layer is the scheduling foundation §5 builds
  on — it does NOT implement §5.
* **FUTURE — broader Apache-2.0 designation** (only with maintainer
  sign-off via LICENSE-MAP.md reclassification): internal/chunking,
  internal/histref.
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
