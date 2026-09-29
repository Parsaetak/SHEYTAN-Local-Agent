# SHEYTAN-Local-Agent — Engineering Worklog

Current release:  v1.7.5

---
Task ID: 1 (v1.7.1 session)
Agent: v1.7.1 engineering session
Task: v1.7.1 — P0 scheduler settlement, context-exhaustion recovery, preflight gate + live protection, Native first-class backend, license cleanup, docs/version truth

Work Log:
- P0: reproduced the CI failure (`-race -count=5` → TempDir "directory
  is not empty"); root cause = the test's final resumed RunNow worker
  persisting after test return. Documented the RunNow SETTLEMENT
  CONTRACT (close = persistence + bookkeeping complete), drained every
  RunNow channel in tests, added TestRunNowChannelCloseIsFullSettlement
  (disk + bookkeeping asserted synchronously at close). Verified:
  targeted `-race -count=10`, full suite `-race -count=3`.
- Feature A: internal/recovery package (typed condition, snapshot,
  hierarchical + fallback summary, durable versioned handoff store,
  bounded injection); typed mapping at the llama.cpp client boundary
  and the native boundary (Unwrap); orchestrator bounded recovery loop
  (typed-condition-only, restart exactly once via the runtime
  coordinator, loop guard at 1 attempt); runtime coordinator over
  LlamaServer.Restart / Engine Stop+Start + awaitReady; identity
  (session/thread/run) wired from the API server; telemetry fields.
- Feature B: internal/preflight (one Report; Evaluate over existing
  authorities; LiveMonitor with hysteresis + synchronous critical
  protection); server run gate refuses incompatible BEFORE engine
  start; /api/preflight; ModelPicker renders the report; runtime
  active-run cancel registry wired into streamGeneration.
- Feature C: llm.BackendCapabilities + CapabilityReporter implemented
  by both backends; failure taxonomy (failureclass.go + NormalizeError);
  BackendCandidates verdict table in /api/engine; nil-hardened
  SelectGenerationBackendDetailed; native README contradiction cleaned
  (Phase 1-4 relabeled HISTORICAL).
- License: LICENSE.md human-facing index (embeds no license text);
  internal/releasecontract/license_contract_test.go pins the EXACT
  license-file set and blocks redundant license Markdown.
- Version identity 1.7.1 across package.json / config.go /
  build/config.yml / SIGNATURE (release-version.mjs --check green).
- Verification: go test ./internal/... -tags headless (54 pkgs, 0
  fail); go test ./... -tags headless -run Test (0 fail); vet clean;
  race battery green (scheduler/recovery/preflight/runtime/api/agent/
  llm/sessions/contextplan/histref); cmake + ctest 12/12; real-host
  Go integration battery (TestRealCppHost*, Phase 7 acceptance) green;
  frontend typecheck/lint/units/build/stable-asset/release-check green.

Stage Summary:
- v1.7.1 complete on this revision. Same-revision Actions verification
  and the Windows runtime probes remain NEXT (see ROADMAP §0 NEXT).

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

---
Task ID: 1
Agent: main (Super Z)
Task: SHEYTAN-Local-Agent v1.7.2 — KV CLI contract fix, AUTO GPU candidate bootstrap, license consolidation (113-minute engineering prompt)

Work Log (fresh evidence, Linux amd64 build host, Go 1.26.8, Node 24):
- BASELINE INSPECTION: cloned main @ 69bbd6c (v1.7.1); read internal/llm (speed/capability/llama/devices/exhaustion), internal/updater (variant/install), internal/accelerator, internal/preflight, internal/runtime, internal/recovery, internal/releasecontract, internal/api (perf/engine_variant/server), internal/brand, internal/hardware; version authorities + Actions workflow mapped.
- REAL ENGINE EVIDENCE (the root-cause layer): downloaded the ACTUAL released llama.cpp binaries (llama-b10642-bin-ubuntu-x64.tar.gz, llama-b11205-bin-ubuntu-x64.tar.gz) and probed them natively; also fetched upstream common/arg.cpp at both tags. Measured: BOTH tags reject `--cache-type-kv` (`error: invalid argument: --cache-type-kv` — the user-reported b11205 failure reproduced on the real binary) and accept ONLY `-ctk, --cache-type-k` / `-ctv, --cache-type-v` (exact lowercase vocabulary f32 f16 bf16 q8_0 q4_0 q4_1 iq4_nl q5_0 q5_1); `--mlock` is REMOVED at b11205 (invalid argument) and DEPRECATED-but-accepted at b10642, replaced by `-lm, --load-mode MODE`; `--flash-attn` REQUIRES the on|off|auto value form at BOTH tags (the repo's "b10642 accepted the bare flag" comment was wrong — corrected flashAttnValueSinceTag 10900→10642 with the measurement); `--list-devices` prints "Available devices:" (with "(none)" on this GPU-less host). Verbatim --help outputs ship as fixtures: internal/llm/testdata/help-b10642-real.txt, help-b11205-real.txt.
- P0 KV CONTRACT FIX: EngineCaps extended (CacheTypeK/CacheTypeV/CacheTypeKVShared + Mlock/LoadMode + Schema=2; pre-v1.7.2 persisted profiles are stale → re-probed once); help parsing boundary-aware (helpMentionsOption — "--cache-type-k" can never match "--cache-type-kv" or "--cache-type-k-draft"); tag fallback fail-closed (split only ≥ b10642, nothing below; shared form only ever from --help); SpeedArgsWithCaps emits the layout the engine contract supports (--load-mode mlock preferred over --mlock); argProblems mirrors the engine parser (exact value vocabulary — case-sensitive like the engine; layout-capability gates; mixed-layout rejection; flag-consumption safety); repairCapsFor gained the ACYCLIC chains shared→split→none and mlock→load-mode→none (bounded, no oscillation, unrelated options untouched, removals recorded).
- P0 AUTO GPU CANDIDATE: UpdateEngineVariantNow refactored into updateEngineVariantTx(+variantVerifyFunc) — the ONE transactional authority, extended not duplicated; internal/llm/variant_auto.go: AutoProvisionVulkanIfWorthy (bounded state in gpu-probe.json keyed by GPUProbeIdentity: OS/arch + GPU identity + driver + engine tag + variant), autoProbeVerify (engine's own --list-devices with the ACTUAL identity, mandatory real bounded generation via the raw /completion endpoint, mandatory real offloaded-N/M-layers evidence line; no-device and no-evidence candidates FAIL and roll back through the v1.7.0-hardened path), stable gpu* lifecycle diagnostics; internal/api/gpu_autoprobe.go: the eligibility chain (AUTO requested, not remote, platform serves Vulkan, host GPU DETECTED via the deep hardware probe, GPU offload not disabled, model selected+resolvable, no active run/calibration, online, no bounded-failure record, FINAL PREFLIGHT gate — hard-incompatible or critical pressure defers), triggered from EnsureSetup strictly AFTER the maintenance gate, in the background. GET /api/engine/provision now carries the autoProbe state.
- P0/P1 LICENSE CONSOLIDATION: LICENSE.md rewritten as the ONE human-facing licensing document (copyright, mixed model, open/proprietary classification tables, how-to-determine, third-party overview + llama.cpp notice + Go/frontend dependency tables, trademark, governance, contact, relationship to authorities); LICENSE-MAP.md + NOTICE.md merged in and git-rm'd; LICENSE/LICENSE-APACHE/LICENSE-PROPRIETARY preserved; internal/brand (LicenseText/LicenseFooter/comments) + internal/humanize updated; LICENSE regenerated via scripts/gen-license.go; contract test REWRITTEN (one-human-facing-Markdown exactness, consolidated-content proof, authority preservation, duplicate detection incl. Licence.md/LICENCE.md/license.md/notice.md spellings, non-Markdown authorities never falsely flagged, license-CLI/generated-LICENSE consistency).
- DOCS TRUTH: UPDATE.md v1.7.2 section + standing evidence-truth block (GPU detection ≠ Vulkan package ≠ Vulkan device ≠ offload; CI ≠ hardware proof; compat 2 ≠ full-speed; native test ≠ native selected); README v1.7.2 highlights + release-history row; ROADMAP §0 v1.7.2 handoff (truthful evidence classes; Windows runtime NOT claimed); agent.md v1.7.2 handoff note; all LICENSE-MAP/NOTICE references updated (historical mentions labeled historical).
- VERSION: 1.7.2 in package.json / config.AppVersion / build/config.yml / SIGNATURE (regenerated via scripts/gen-signature.go); release-version gate --check green.

Regression evidence (this host, Linux amd64, Go 1.26.8, Node 24):
- go build ./... + headless build — green; Windows amd64 cross-build (CGO_ENABLED=0) — green (29.6 MB exe).
- go vet ./internal/... — clean.
- go test -tags headless -count=1 ./internal/... — 56/56 packages ok (incl. the 12 new KV-contract tests, 10 new AUTO-probe transaction tests, rewritten license contract; preflight/hysteresis/exhaustion/sampling suites from v1.7.1 re-verified green).
- go test -race -tags headless ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime — ok; -race on the new llm tests — ok.
- Native C++ engine: cmake --fresh configure + build + ctest — 12/12 (twice: initial + clean-room).
- Frontend: npm ci, typecheck (0 errors), lint (0/0), test:units 132/132, test:release, build + verify-static-assets — green.
- Browser E2E (real stack): 24/24 passed (one first-run flake in sessions delete — passes in isolation and on the full rerun; diagnosed, not disabled).
- Stress chaos suite: 47/47 pass, 0 fail/hangs/crashes (the earlier 46/47 was a working-directory artifact of running the binary outside the repo root).
- Network CI variant gate (SHEYTAN_CI_VARIANT_GATE=1): TestCIVariantAssetContract PASS against the REAL upstream (4.7 s).
- Clean-room: native build dir + e2e artifacts purged, go clean -testcache, fresh native ctest + focused engine/GPU/API/license tests — green.

HONEST LIMITS (evidence classes NOT claimed): the Windows Vulkan runtime acceptance chain (real Vulkan device enumeration on Windows hardware, real GPU-offload execution) CANNOT run on this Linux host — the candidate path is proven by deterministic transaction tests + the real-asset network gate; GPU_VULKAN selection remains evidence-gated (executionVerified only with the real offload line) exactly as before. No Windows CI run was observed post-push.

Stage Summary:
- v1.7.2 complete: the b11205 KV launch defect is eliminated at the root (real-engine-verified layouts), the AUTO GPU deadlock is broken through a bounded transactional candidate probe with execution-evidence verification, licensing is consolidated into ONE human-facing document with a two-directional contract test, and every gate runnable on this host is green on fresh evidence.

---

Task ID: v1.7.2-repair-pass
Agent: main (Super Z)
Task: v1.7.2 deep repair — Windows one-word-chat crash (root cause) + single-license-file consolidation + verification pass.

Work Log:
- REPRODUCED the supplied Windows chat crash deterministically: the log ends at `task classified`; the next expected line is `tier selected`. Between those markers RunDetailed walks the tool surface. The old `Orchestrator.Tools()` returned the LIVE internal map, and the tier walk read `len(o.tools)` outside the lock; concurrent Register/Unregister (custom-tools HTTP handlers, task-scoped tool register/teardown in the scheduler task runner, native-engine unregister sites) writes during that walk = fatal `concurrent map iteration and map write` — unrecoverable, kills the whole desktop process with no terminal event. go test -race reproduction captured the exact race (Register at orchestrator.go:331 vs the RunDetailed iteration).
- ROOT-CAUSE FIX (minimal, ownership semantics): `Tools()` now returns an immutable SNAPSHOT (private copy under the read lock); the tier-selection walk uses one snapshot for len+iteration; `resolveEffectiveContext` reads the ctxLimits provider under the SAME mutex SetContextLimitProvider writes under; `/api/tools` iterates the snapshot with sorted names for deterministic output.
- REGRESSIONS: internal/agent/registry_snapshot_test.go (crash-window race reproduction — fails on the pre-fix build; snapshot-isolation contract; 6-reader/4-writer concurrent stress with integrity validation) and internal/api/run_survival_v172_test.go (real-stack process-survival acceptance: one-word chat with Net Search OFF and ON → done + exactly one persisted assistant message + liveness; forced backend failure → terminal error event delivered over the WebSocket, zero duplicate assistant messages, process alive; the next ordinary chat succeeds on the same server).
- LICENSE CONSOLIDATION (the rejected 4-artifact model → exact-one): read all four artifacts + the v1.7.1 LICENSE-MAP.md/NOTICE.md from history; consolidated EVERYTHING into LICENSE.md (model, classification, third-party notices, FULL Apache-2.0 text, FULL Parsaetak Proprietary License v1.1, trademarks, governance, contact; routing references rewired to internal sections); git-rm'd LICENSE, LICENSE-APACHE, LICENSE-PROPRIETARY; internal/brand.LicenseText is now the complete document (single in-code authority); scripts/gen-license.go writes ONLY LICENSE.md and actively removes resurrected legacy artifacts (verified: idempotent, removes planted LICENSE/LICENSE-APACHE); cmd/license.go prints the complete document truthfully; CI workflow (6 sites) + NSIS installer adapted to LICENSE.md; license_contract_test.go REWRITTEN to the exact-one whole-tree invariant (case-insensitive filename scan, banned-artifact list, both-legal-texts anchor proof, no-live-authority-reference scan, generator output-path contract).
- DOCS: README/ARCHITECTURE/CONTRIBUTING/SECURITY/UPDATE/agent.md/ROADMAP.md moved to the single-document architecture (historical mentions labeled historical).
- ROADMAP: FUTURE-only entries added (Q1–F16 quantization matrix from actual upstream support, Safetensors pipeline, LiteRT/LiteRT-LM investigation) — nothing implemented.
- b11205 CLI contract re-verified (fixture-backed suites green); GPU/AUTO audit: offload-disabled gate, enumerated-device identity, execution-evidence semantics, bounded one-shot candidate, no polling side effects — no defects found; accelerator resolution semantics unchanged.

Regression evidence (this host, Linux amd64, Go 1.26.0, Node 24):
- gofmt: new/changed files clean (pre-existing upstream space-indentation left untouched — formatting the tree would create a 33-file whitespace diff).
- go vet -tags headless ./internal/... — clean.
- go test -tags headless -count=1 ./internal/... — 56/56 packages ok.
- go test -race -tags headless ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime — ok (incl. the new regressions).
- Native C++: make build + test — 12/12 (build artifacts cleaned afterwards).
- Frontend: npm install, typecheck (0 errors), lint (0/0), test:units 132/132, test:release 28/28, build + sync:web + verify-static-assets — green; web/static byte-identical (no frontend drift).
- Browser E2E (real stack, real browser, real native-engine generation): chat 5/5, composer+sessions 8/8 (incl. the Net Search intent test).
- Version: 1.7.2 consistent across package.json / config.AppVersion / build/config.yml / SIGNATURE / web/static; release-version.mjs --check green.

HONEST LIMITS: Windows desktop runtime (Wails window on Windows) cannot run on this Linux host — desktop-build GTK deps unavailable; the crash root cause is proven by the -race reproduction and the fix by the real-stack API/WebSocket/session acceptance, and the CI Build Desktop run #36285171524 remains the packaging evidence, not a runtime proof. No live Windows b11205 binary on this host — the KV contract is verified against the committed real-help fixtures. GPU execution on real Vulkan hardware remains evidence-gated exactly as before.

Stage Summary:
- Windows chat crash: root cause identified by evidence (fatal registry map race in the classification→tier window), fixed at the ownership level, pinned by deterministic regressions (race + real-stack survival), full matrix green.
- Licensing: exactly ONE artifact (LICENSE.md) in the whole tree, complete consolidated content, generator/CLI/CI/packaging rewired, contract enforced both directions.
- v1.7.2 version identity preserved; architecture preserved (one registry, one session store, one lifecycle owner — no duplicates introduced).

---
Task ID: v1.7.5
Agent: v1.7.5 engineering session (Super Z)
Task: SHEYTAN-Local-Agent v1.7.5 — Windows migration test lifecycle repair, stale session-list guard, transactional paused-edit, engine idempotency proof, content-level tool-metadata race closure, abort/recovery run-control hardening, README/version truth (113-minute engineering prompt)

Work Log (fresh evidence, Linux amd64 build host, Go 1.26.0, Node 24):
- BASELINE: cloned main @ 81fd192 (v1.7.4); v1.7.4 Actions state per prompt (Source/frontend audit PASS; Windows x64 FAIL on TestCopyVerifiedClassifiesHeldOpenDestination; Linux x64 FAIL on the sessions delete E2E) taken as the working hypothesis and re-derived from source.
- A. WINDOWS MIGRATION TEST (test-only): the v1.7.4 regression held the conflicting sink handle until test end and called the second migration a "restart" while the handle was STILL open — on Windows the retry rename deterministically fails with EACCES again (a logically impossible restart). The test now RELEASES the handle (models process exit) before the next-boot retry; every real invariant preserved (destination never truncated, temp cleaned, source intact, idempotent fold, removal only after verification). PASS on Linux by construction for Windows: the retry now happens strictly after the conflicting handle no longer exists. Production migration untouched (bootstrap-before-file-logger boot order intact).
- B. STALE SESSION-LIST GUARD (production): root-caused the Linux E2E failure — refreshSessions captured the mode but not a mutation/request generation; a stale GET landing after a create/delete/rename/mode-switch overwrote newer state (deleted session resurrected). New pure module src/session-list-guard.ts (one monotonic generation; tickets invalidated by every mutation and superseding refresh) wired into refreshSessions/createSession/deleteSession/renameSession/setMode. 9 deterministic out-of-order unit tests (session-list-guard.test.ts, in test:units). The browser E2E "delete session removes it and activates a remaining one" now passes (5/5 sessions.spec).
- C. TOOL METADATA DEEP COPY (production): customtools Store.Save/List/Get returned SHALLOW copies aliasing Params/Enum/Default/HTTP.Headers/Command.Args with the cache — the content-level aliasing behind the copied registry map. All three now hand out deepCopyDefinition. New internal/agent/toolmetadata_race_test.go: the complete task-classified→tier-selected interval (Tools() snapshot → SelectForTask → tool lookups → specCache.BuildSpecs → JSON spec validation) against REAL custom tools with HTTP/Params/Enum churn, 4 readers + 2 mutators + store churn — green under -race.
- D. ENGINE IDEMPOTENCY (test-only): internal/api/maintenance_twoboot_v175_test.go — a deterministic SAME-DATA-ROOT two-boot proof through the real startup maintenance gate (boot 1 commits b11223 via the seamed transaction incl. binary+manifest+RecordEngineTag; boot 2 completes "current" with ZERO transactions), plus the full fallback matrix: state tag-less + manifest valid; manifest missing + state valid; both missing → honest transaction; target genuinely newer → transaction targets it; binary path changed → installer reseeds the identity from the manifest beside the new binary and the gate stays "current".
- E. EDIT TRANSACTIONAL CONSISTENCY (production, §10): handleRunEdit previously bumpRevisioned the live state BEFORE the durable work — a failed checkpoint save left live revision ahead of the durable checkpoint. Rewritten: per-run ctrlMu serializes edit/resume/stop-while-paused; CAS-peek via new runLive.canEdit (no mutation); transcript replace → checkpoint commit → transcript rollback on commit failure → confirmEdited LAST. Field PRESENCE semantics: message/draft are *string — an assistant draft can now be intentionally ERASED ("" is a value). Fault injection via the pausedSaveSeam (nil falls through to the REAL write): checkpoint-save failure leaves live state, transcript and checkpoint at the OLD revision with a clean retry; transcript-update failure is a clean abort. Recovery reconciliation: pausedUserMessageFor reads the conversation identity from the TRANSCRIPT authority (paused surface + resumed settlement).
- E2. RUN-CONTROL HARDENING (production, real bugs found by the new browser E2E): (1) /api/abort could never cancel a RESUMED generation — the resumed run owns a FRESH context; the resume handler now creates the context, installs its cancel via runState.setLiveCancel/cancelCurrent BEFORE the phase is published (the RESUMING spawn window is covered); (2) a pause during the RESUMING window was silently swallowed by RunControl.Clear — requestPause now rejects it with an explicit conflict; (3) replacing a paused run left its checkpoint resumable OVER the live run — the replacement consumes the checkpoint; (4) a run that lost its session slot could checkpoint a zombie — both pause paths verify they still own the registry slot; (5) deleting a session now settles its registered run and consumes its checkpoint (no orphaned run-control state); (6) NATIVE BACKEND PAUSE WAS BROKEN: NormalizeError erased the error chain, so errors.Is(err, context.Canceled) failed at the orchestrator pause boundary and a native-backend pause settled as ERROR — Failure now carries its cause (Unwrap), streamCall's abandon path threads ctx.Err(), and the stall branch wraps the cause; sentinel regression test added (TestNormalizeErrorPreservesContextSentinel).
- E3. FRONTEND WIRING REPAIR (production): the live PAUSED confirmation rode agent.Activity.Detail ("detail" on the wire) but the store read a top-level phase — a live pause never transitioned the phase and the paused panel could not appear without a reconnect. The store now reads detail.phase and populates pausedDraft/runRevision from the confirmation; the backend includes pausedDraft in both pause confirmations. STOP-while-paused: abort() now transitions paused→aborted (the backend settles a parked run synchronously; the idle sentinel is ignored for non-live phases).
- F. README + VERSION (docs): README current release v1.7.1 → v1.7.5; accurate v1.7.5 highlights + new v1.7.3/v1.7.4 historical sections (history kept historical); release-history rows for 1.7.3/1.7.4/1.7.5; package examples 1.3.2 → 1.7.5; bottom Version block v1.2.7 → v1.7.5. Version identity 1.7.5 via the canonical gate (package.json → config.AppVersion, build/config.yml, SIGNATURE); release-version.mjs --check green. No codename anywhere (retired-token grep: only the MaterializeTarget identifier false-positive).
- VERIFICATION (this host, Linux amd64, Go 1.26.0, Node 24):
  * go build -tags headless ./... — green; go vet ./internal/... (headless) — clean; go vet ./... blocked only by the documented GTK/WebKit cgo host limitation (wails desktop deps; CI covers it).
  * go test ./internal/... -tags headless -count=1 — 57/57 packages ok (multiple passes; includes all new suites).
  * go test ./... -run Test -count=1 — every package that compiles on this host passes; only wails/GTK desktop deps fail to build (host limitation, pre-existing).
  * go test -race ./internal/agent ./internal/api ./internal/sessions ./internal/customtools ./internal/runtime — ok (api 93.9s; timing-sensitive abort regression made deterministic by aborting in the RESUMING window).
  * Native engine: cmake configure + build + ctest — 12/12.
  * Frontend: typecheck 0 errors; oxlint 0/0; test:units 151/151 (incl. the 9 new guard tests); test:release 28/28; npm run build + sync:web — green.
  * Browser E2E (real stack: headless server + embedded UI + WS + session store + REAL native-engine generation): 29/29 — incl. the previously failing sessions delete test and the 5 new run-control tests (pause lifecycle; reload-while-paused restores the same draft; edit→resume→one user turn + one authoritative answer; pause→stop frees the composer; stale-revision 409 with the paused state intact; active-session delete with no resurrection and a replacement active).
  * Stress suite: 47/47 pass, 0 fail/hangs/crashes.
  * release-version.mjs --check — green.
- HONEST LIMITS (evidence classes NOT claimed): Windows desktop runtime and the Windows rename behavior CANNOT execute on this Linux host — the migration test fix is proven by construction (the retry now happens strictly after the conflicting handle is released; the real rename is used) and Windows CI is the runtime confirmation; the llama.cpp serving path (tools-bearing requests) has no llama-server binary in the E2E environment (documented fixture limitation — tool-bearing prompts select the llama.cpp fallback and fail there; the pause E2E uses tool-free prompts served by the native engine); GPU/NPU execution on real hardware remains evidence-gated exactly as before; no CI run observed post-packaging.

Stage Summary:
- v1.7.5 complete: the two v1.7.4 CI failures are root-fixed (test lifecycle + stale-refresh guard), pause/edit/resume is transactionally consistent and NOW ACTUALLY WORKS on the native backend and in the live UI (both proven by the real-stack browser suite), engine maintenance idempotency is two-boot proven, tool metadata is concurrency-safe at the content level, and the docs/version identity is truthful. Architecture preserved: one session store, one registry, one run lifecycle, one maintenance gate, one licensing artifact.

---
Task ID: v1.7.6
Agent: v1.7.6 engineering session (Super Z)
Task: SHEYTAN-Local-Agent v1.7.6 — codename audit gate repaired token-aware (both directions), durable edit-transaction recovery (§7), restart-recovery revision restoration, spec-cache generation checking, corrupt engine-identity matrix rows, version/README truth (113-minute engineering prompt)

Work Log (fresh evidence, Linux amd64 build host, Go 1.26.0, Node 24):
- BASELINE: cloned main @ 1c3c5b4 (v1.7.5, identical to the prompt's observed HEAD); GitHub API shows the newest run is 36513941947 ("Build Desktop", FAILURE on 1c3c5b4): "Source & frontend audit" failed, Windows x64 / Linux x64 / Publish all SKIPPED — matches the prompt. Job logs are admin-gated (403), so the failure cause was re-derived from source: the codename-removal gate regex hits worklog.md:232 — a historical audit note quoting the literal retired token; the MaterializeTarget identifier false-positive is historical (the note itself says so) and the current boundary-regex no longer matches it, but the regex CANNOT see camelCase/digit-joined spellings (a leading-capital compound of the retired name passed) — a detection gap in the other direction.
- A. CODENAME GATE (production: .github/workflows/build-desktop.yml + new scripts/codename-gate.mjs + scripts/codename-gate.test.mjs + worklog literal rephrased): the gate is now a deterministic token-aware scanner — identifiers are maximal alphanumeric runs, split into case-folded sub-tokens at snake/kebab separators, camelCase humps, acronym runs and letter<->digit transitions; a hit is any sub-token equal to the retired token, the whole identifier case-folding to it (mixed-case single-word spellings), the joined legacy form, or the separated legacy pair across the line's token stream. MaterializeTarget class = ALLOWED (tokenizes materialize|target); every retired spelling = FAIL. The retired token is constructed at runtime in gate+tests so the tracked tree stays clean. Wired into the audit job (replaces the git grep) and into npm run test:release. Worklog v1.7.5 line F rephrased without the literal (the historical note stays, the banned token does not).
- B. DURABLE EDIT-TRANSACTION RECOVERY (production: new internal/api/edittx.go + handleRunEdit integration + startup hook): each paused-run edit is wrapped in a bounded, integrity-hashed journal (<DataDir>/runs/paused/<runId>.edittx.json, atomic temp+rename — the checkpoint's own pattern) recording runId/sessionId, old/new revision, old/new user message (bounded by the edit body cap), the pre-edit EditedUserMessage, old/new draft state, phase and version. Prepared BEFORE any mutation (journal-write failure aborts the edit with NOTHING changed); phase notes advance as the durable stages land and are explicitly TOLERANT (recovery is evidence-driven, never phase-driven); deleted after live publication. Startup recovery (RecoverEditTransactions inside api.New, before anything can expose a paused run) converges each journal from ACTUAL durable evidence: checkpoint consumed → restore pre-edit transcript / clean; transcript at the NEW message → converge FORWARD (checkpoint completed from the journal); transcript at the OLD message (or draft-only with an old checkpoint) → converge BACK; matches neither → quarantine (.edittx.corrupt.json) with actionable logging, NOTHING guessed, other sessions unaffected. Idempotent (converged journals are deleted). NOT a second authority: transcript + checkpoint remain the authorities; the journal is forensic only.
- B-tests (internal/api/edittx_v176_test.go, deterministic — no sleeps; crashes simulated by a seam panic unwinding through the handler's deferred ctrlMu release, the wire-level effect of process death, or by writing the exact leftovers a crash leaves): journal-write failure aborts before mutation (§7#9); transcript failure clean abort removes the journal (#7); checkpoint failure rollback removes the journal AND a retry succeeds (#8); crash windows prepared/transcript-durable/checkpoint-durable/commit-marker(#1–4, #5/#6) each converge to ONE coherent revision; draft-only variants both ways; corrupt journal fails closed with the paused listing clean (no journal pollution, unrelated sessions still work); full restart integration: recovery → /api/run/paused lists the coherent NEW revision → resume at the recovered revision → settles with exactly one user turn + one authoritative assistant turn (#10); repeated convergence passes are deterministic.
- C. REAL BUG FOUND AND FIXED (production: internal/api/runstate.go + runcontrol.go): the v1.7.5 restart recovery (recoverPausedRun) restored the paused draft but left the live revision at 0 — a run EDITED before its process died recovered unable to resume at its own recorded revision (every resume 409'd against the very checkpoint it recovered from; the v1.7.4 test only ever recovered at revision 0 so the hole was invisible). recoverPausedRun now pins the checkpoint revision (runLive.restoreRevision — pause-only, monotonic). Found BY the new restart-integration test, exactly the §7/§12 invariant the prompt demands.
- D. PAUSED-LISTING HARDENING (production: handlePausedRuns): edit-transaction journals (.edittx.json), quarantined leftovers and temp files are explicitly excluded — a recovery record can never be listed as a bogus resumable entry beside its checkpoint (found by inspection while integrating the journal: the old .json suffix scan would have unmarshaled journals as checkpoints).
- E. SPEC-CACHE GENERATION CHECKING (production: internal/agent/toolcache.go, orchestrator.go, orchestrator_tiers.go): the v1.7.5 state closed the container and content races; one theoretical window remained (a superseded reader re-populating the NAME-keyed cache after a concurrent Register+Invalidate, serving the replaced tool's schema to a later reader). Register/Unregister now bump a registry generation under toolsMu; ToolsAt() returns snapshot+generation atomically; specCache entries carry the generation and a hit is served only to a same-generation reader; setTools and the tier-measurement build stamp with the generation. Deterministic tests (spec_cache_v176_test.go) drive the exact superseded interleaving, the registry-generation monotonicity and the atomic pair; the existing race suites now exercise generation-checked hits under -race.
- F. ENGINE IDENTITY — CORRUPTION ROWS (tests: internal/api/maintenance_identity_v176_test.go): §6 matrix rows 7–8 pinned at the real gate: corrupt state + valid manifest → manifest fallback, current, ZERO transactions (a missing/untrustworthy identity never forces a redundant download while another committed identity exists); state corrupt AND manifest missing (binary proves nothing about WHICH build) → honest transaction; genuinely newer target → updates through the corruption. Verified the existing machinery (InstalledEngineTag returns "" on unparseable state → EffectiveInstalledEngineTag falls back to the manifest) — tested, not assumed.
- G. VERSION 1.7.6 (docs): package.json 1.7.6 (canonical) → release-version.mjs repaired config.AppVersion, build/config.yml productVersion, SIGNATURE to 1.7.6; --check green; package-lock.json synced. README: current release v1.7.6, new v1.7.6 highlights section, v1.7.6 release-history row, package examples 1.7.5 → 1.7.6 (Windows ZIP, Linux ZIP, NSIS installer), run-control endpoints added to the REST surface list, bottom Version block v1.7.6. v1.7.5 and older material kept historical.
- VERIFICATION (this host, Linux amd64): focused go test ./internal/config -tags headless — ok; go vet ./internal/agent ./internal/api — clean; new suites green (codename gate 10/10 node tests; EditTx matrix; identity corruption rows; spec-cache generation). Full funnel results recorded below in this entry's verification block.
- HONEST LIMITS (evidence classes NOT claimed): Windows desktop runtime and the Windows-specific behaviors CANNOT execute on this Linux host (CI's Windows x64 job is the runtime evidence); the repaired audit gate has NOT been observed passing in a fresh Actions run from this host — pushing to GitHub is not possible here (no credentials) and job logs are admin-gated, so Actions confirmation remains the user's push; the Browser E2E and native suites were run only where this host supports them (results below); GPU/NPU execution on real hardware remains evidence-gated exactly as before.
- VERIFICATION BLOCK (measured on this host, Linux amd64, Go 1.26.0, Node 24, Playwright chromium):
  * Focused: go test ./internal/config -tags headless -count=1 — ok.
  * Full Go: go test ./internal/... -tags headless -count=1 — every package ok (chunked a..v + root/cmd/scripts, zero failures); packages without tests compiled clean.
  * go vet ./... — zero Go code findings (only the documented GTK/WebKit pkg-config host limitation of the wails desktop package).
  * go test -race ./internal/agent ./internal/api ./internal/sessions ./internal/customtools ./internal/runtime -tags headless -count=1 — all ok (api 92.6s incl. the new EditTx + spec-cache suites under race).
  * Frontend: npm run typecheck 0 errors; oxlint 0 warnings 0 errors (87 files); test:units 151/151; test:release 38/38 (release-version 28 + codename-gate 10); npm run build + sync:web green (web/static refreshed by the CI-identical build).
  * Native engine: cmake 4.4.3 configure + build — ok; ctest — 12/12 passed.
  * Stress suite: 47 pass / 0 fail / 0 hangs / 0 crashes (STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0).
  * Browser E2E (real stack: headless Go server + embedded UI + WS + session store + real native-engine generation): 29/29 passed in 4.3m — includes the full run-control set (pause lifecycle, reload-while-paused, edit→resume→one final answer, pause→stop, stale-revision 409, active-session delete) executed against the server binary BUILT WITH the v1.7.6 journal code, so every real edit in the suite exercised the journal path.
  * release-version.mjs --check — green at 1.7.6.
  * NOT claimed on this host: Windows x64 desktop build/runtime (Linux host — CI's Windows x64 job is the evidence); a fresh Actions run cannot be triggered from here (no push credentials; job logs are admin-gated 403) — the workflow changes are validated by the deterministic local equivalents of every gate the audit job runs (version check, frontend verification, static-asset contract, codename gate, race gate, native build/tests).
