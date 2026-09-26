# SHEYTAN-Local-Agent — Engineering Worklog

---
Task ID: 1
Agent: main (Super Z)
Task: P0 — Windows build/rollback repair for CI run 36226617025

Work Log:
- Cloned repo at 2c4e8bb (v1.6.2), installed Go 1.26.0, built with `-tags headless` (GTK/WebKit unavailable on the build host; desktop targets compile via CI).
- Reproduced the v1.6.2 variant transaction on Linux: the failing Windows test passes on Linux because Linux allows rename/unlink of a running executable's tree — the defect (rollback under a live candidate process) is deterministic and now regression-guarded on every OS.
- Root cause: `UpdateEngineVariantNow` called `staged.Rollback()` while the just-started candidate engine still owned the managed `bin` tree. On Windows the running llama-server locks its executable image + DLL closure → `rename ...bin.update-old ...bin: Access is denied`.
- Fix (`internal/llm/llama.go`): new lifecycle-owned `stopCandidateForRollback` (SIGTERM → bounded grace → Kill → deterministic reap on exitDone; cancels an armed watchdog; resets the deliberate-stop marker) invoked BEFORE any rollback filesystem mutation, in BOTH the verification-failure and startup-failure paths of `UpdateEngineVariantNow` and `UpdateEngineNow`. No sleeps, no taskkill, no polling.
- Rollback branches now report explicit restart evidence (`(previous package restored; last-known-good restart failed: …)`) and set `StateFailed` only when nothing serves — a successful LKG restart stays honest (`ready`).
- Regression suite (`internal/llm/variant_rollback_v170_test.go`): candidate stopped before restore (deterministic probe seam `variantRollbackProbe` fires at the exact rollback instant), rollback succeeds, previous package byte-identical (tree hash), manifest stays authoritative, LKG restarts healthy, port free after final stop (no orphans), commit path never enters the rollback seam. Existing v1.6.2 suite untouched and green.

Stage Summary:
- Full `internal/llm` suite: PASS (51s, Linux). Windows CI gate unblocked by construction: no live process may own the bin tree at rollback time, on any platform.

---
Task ID: 3
Agent: main (Super Z)
Task: P1 — Chronological tasks + scheduling + automation

Work Log:
- Extended the ONE scheduler (`internal/scheduler`): same store, same durable-claim discipline, no second engine.
- Task model v1.7.0: Paused gate, Schedule (once / interval / daily / weekly at LOCAL time), LinkedSkills, TaskTools, TaskTypes, LastRun; serialized duration companions (intervalSeconds/maxRuntimeSeconds) for JSON round-trip.
- New operations: RunNow (async, bounded, deterministic refusal when running/paused), Pause/Resume (resume recomputes NextDue from now — no stale burst), CancelRun, UpdateTask (revalidates the AddTask contract, resets the timeline on schedule change), Runs (per-task chronological history), NotifyEvent, ShutdownSettle.
- Durable claim extended to every schedule kind: recurring claims persist the advanced NextDue before execution; once-tasks durably disable themselves before execution — a crash can never replay the same claimed deadline. Missed-deadline policy: a past deadline runs EXACTLY ONCE, next occurrence strictly after the claim instant.
- Event triggers: NotifyEvent(kind) fires enabled, unpaused, not-running tasks (file_change / git_change / test_failure / ci_failure / build_failure declared; genuine emitters wired in Task 7).
- API (`internal/api/automation.go`): full CRUD + run/pause/resume/cancel/runs + task-scoped tools + artifacts surfaces; bounded payloads, deterministic errors, cancellation support.
- Tests (`automation_test.go`, `automation_api_test.go`): create/update/delete, once, recurring daily/weekly local-time math, missed-deadline run-once, pause gates, manual run, event trigger, cancellation, durable claim (claim persisted while run is in flight), restart/reload, duplicate prevention, metadata round-trip, real HTTP paths. Race-clean.

Stage Summary:
- Scheduler = v1.7.0 automation authority; every guarantee test-proven; Tick API byte-compatible with v1.2.9 claims.

---
Task ID: 4
Agent: main (Super Z)
Task: P1 — Markdown SKILL.md packages + progressive disclosure

Work Log:
- `internal/skills/markdown.go`: SKILL.md packages (frontmatter + Markdown body) parsed/validated (path-safe ids, bounded size, required name/description/triggers, references must not escape the package dir).
- Discovery is METADATA-ONLY (progressive disclosure step 1); bodies load on match (step 3); declared references load only when required (step 4); malformed packages are skipped honestly, never fatal.
- Scopes: global (<DataDir>/skills), workspace (<workspace>/skills), task (<DataDir>/task-skills/<taskID>). Task > workspace > global precedence on id collision.
- ToSkill() converges markdown packages into the SAME Skill authority the JSON store serves (no duplicate matching engine for consumers); the JSON path is untouched.
- skill_create agent tool (`internal/tools/skill_create.go`): validated, task-scoped-only creation; immediate usability after validation; NO automatic global promotion — PromoteTaskSkill enforces the existing VERIFIED-learning rule (claimed/partial evidence refused).
- Tests (`markdown_test.go`): valid/invalid markdown, metadata discovery, trigger matching + scope precedence, progressive loading, malformed frontmatter, path traversal, task-scoped lifecycle, verified promotion, unverified rejection, JSON-compat regression.

Stage Summary:
- SKILL.md system live; context stays bounded; promotion gated by verified evidence.

---
Task ID: 5
Agent: main (Super Z)
Task: P1 — Task-scoped custom tools + first-class task/run artifacts

Work Log:
- `internal/customtools/tasktools.go`: TaskDefinition (same Definition contract + TaskID/RunID/Approved), TaskStore under <DataDir>/custom-tools/task-scoped (no second storage root), full lifecycle CREATE → VALIDATE → OPTIONAL APPROVAL → REGISTER → EXECUTE → CAPTURE → CLEAN UP. Defaults: disabled AND unapproved; RegisterInto installs only approved+enabled tools into the ONE orchestrator registry via a cycle-free RegistrarFuncs adapter; teardown unregisters deterministically.
- Real execution loop E2E (`internal/agent/tasktool_e2e_test.go`): model → task tool offered (approved only) → existing registry → executor → result in the follow-up turn → teardown removes the tool; unapproved tools never offered; CleanupTask removes the scope.
- `internal/artifacts/taskmeta.go`: durable TaskRegistry (registry.jsonl) with task/run/source/created/kind/path/size/version/hash provenance; atomic writes; version replacement archives the superseded file (.versions/) so EVERY version stays readable; path-safe filenames, traversal rejected, 8 MiB bound, restart-safe sequence numbers; CleanupTask for task teardown.
- `internal/tools/artifact_create.go`: the agent-facing validated creation operation — task-scoped (refuses outside a task run), path-safe, bounded, atomic, registered with provenance.
- `internal/tools/taskctx.go`: per-run task context installed by the runtime's task runner; artifact/skill creation tools self-gate on it.
- Tests (`tasktools` via agent E2E, `taskmeta_test.go`): create/read/list, markdown first-class, version replacement + readable history, task ownership, path safety, invalid bounds, atomic failure (no temp leaks), restart persistence, cleanup, kind coverage.

Stage Summary:
- Task tools and artifacts flow through the EXISTING executors/registries; safe defaults; every artifact version durable and visible.

---
Task ID: 7
Agent: main (Super Z)
Task: P1 — Connect the four systems (runtime wiring + integration E2E)

Work Log:
- `internal/runtime/automation.go`: wireAutomation installs the shared authorities on the Stack (TaskTools, Artifacts registry, MarkdownSkills with workspace scope), registers artifact_create + skill_create in the orchestrator, and wires GENUINE event emitters:
  * startup — the application booted (NotifyEvent at wiring time);
  * file_change — a file a tool created (tools.OnFileCreated hook, fired on real writes);
  * git_change — a clone genuinely succeeded (API finalizeClone → Stack.NotifyGitChange);
  * test_failure — a Lab verification genuinely failed (lab.OnVerificationSettled hook at the ONE settlement point).
  ci_failure/build_failure remain declared kinds with no synthetic emitters (documented in ROADMAP NEXT).
- taskRunner replaces the bare scheduleRunner wrapper: per-run task context, task-scoped tools installed/unregistered around the run, linked JSON skills + task-scoped SKILL.md bodies injected as a BOUNDED block (progressive disclosure), artifact-count provenance echo appended to the run output.
- Integration E2E (`internal/runtime/automation_test.go`): scheduler task → taskRunner → real agent loop → artifact_create called by the model → artifact in the durable registry with full provenance → run output carries the artifact echo; task-scoped skill discovered and scoped; artifact_create refuses without a task context.
- API shares the runtime's instances (one TaskStore, one registry) — no parallel state.

Stage Summary:
- The v1.7 flow (task → schedule/event → agent run → task skills → task tools → artifacts → history) is wired through the existing authorities and test-proven end to end.

---
Task ID: 8
Agent: main (Super Z)
Task: Regression verification + clean-room build evidence (v1.7.0)

Work Log (fresh evidence, Linux amd64 build host, Go 1.26.0, Node 24):
- go build -tags headless ./... — green.
- go test -tags headless -count=1 ./internal/... — green: 52 packages ok, exit 0.
- go vet -tags headless ./... — clean.
- go test -race -tags headless ./internal/scheduler ./internal/llm ./internal/artifacts ./internal/customtools ./internal/skills ./internal/api ./internal/runtime — ok. NOTE (honest): internal/api under -race intermittently fails TestMaintenanceRepeatedStartupIsSingleFlight with a t.TempDir cleanup race ("directory not empty"). This flake REPRODUCES on the unmodified v1.6.2 baseline (git stash → run → pop), i.e. it is pre-existing and NOT introduced by v1.7.0; the test passes reliably without -race and in isolation. Tracked as a v1.7.1 candidate fix.
- Frontend: npm run typecheck (0 errors), npm run lint (0 warnings / 0 errors), npm run test:units (132/132 pass incl. 10 new automation-view tests), npm run build (tsc -b + vite + sync:web — green; fixed one undefined-prop defect in AutomationPanel caught by tsc -b).
- Release metadata: node scripts/release-version.mjs --check — consistent at 1.7.0; npm run test:release — 28/28.
- Native C++ engine: make -C native/engine test — 12/12 suites, all checks passed (engine, protocol, host, gguf, model, tokenizer, kv_cache, scheduler, sampler, tensor, forward, generate).
- Browser E2E (real native engine host + headless server): 24/24 passed (full suite rerun; the first cold-start run flaked 3 tests on the constrained host — per-spec and full reruns pass).
- Windows CI gate: the rollback repair is regression-guarded on every platform by the rollback-instant probe suite; the Actions windows-latest job must confirm green post-push (no Windows runner on this host — recorded as ROADMAP NEXT 1, not claimed completed).

Stage Summary:
- Every verification gate that can run on this host is green on fresh v1.7.0 evidence; the only outstanding item is the Windows CI re-run (cannot execute here) plus the pre-existing api -race flake.
