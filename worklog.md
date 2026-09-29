# SHEYTAN-Local-Agent — Engineering Worklog

Current release:  v1.8.0

This worklog is a session log, not a second architecture document. The
architecture truth lives in `ARCHITECTURE.md`, the release evidence in
`UPDATE.md`, the future in `ROADMAP.md`.

---
Task ID: 1 (v1.8.0 session)
Agent: v1.8.0 engineering session
Task: v1.8.0 — P0 pause/resume synchronization repair, honest abort
marker, Runtime Governor vertical slice (Adaptive Runtime Intelligence),
documentation consolidation, version truth

Work Log:
- Inspected the live repository at HEAD `eab6e1b` (`v1.7.6`) and the
  failing Actions run `36553559366` (Windows x64 Go verification:
  `TestPauseResumePauseAgainThenResumeCompletes` — `run settled "done"
  (error "") while waiting for phase "paused"`).
- Root-caused the CI failure: the test reused `waitForRunResponse` after
  a resume; its `LatestResponse != ""` condition is satisfied by the
  STALE pre-resume cumulative snapshot, so the second pause was issued
  without proof the resumed generation was live. Reproduced the defect
  class locally; on Linux the failure is scheduling-dependent (the same
  structural hazard), on the Windows runner it deterministically failed.
- P0 repair (no production weakening): added `resumedGenerationEvidence`
  (strictly newer run sequence AND changed cumulative response/reasoning
  snapshot) + `waitForResumedGeneration` (fail-fast on settlement);
  `TestPauseResumePauseAgainThenResumeCompletes` now captures the
  pre-resume snapshot and requires new authoritative activity before the
  second pause. Hazard note added to `waitForRunResponse` (initial
  generation only). Regression coverage:
  `TestResumedGenerationEvidenceContract` (deterministic unit — stale
  cumulative state, seq-only bumps, shorter continuation captions),
  `TestPauseResumeRequiresNewGenerationEvidence` (integration replay of
  the CI scenario).
- P0 (latent, found during the audit): abort-after-resume was flaky at
  BASELINE (~1-in-6 on Linux). Root cause: the orchestrator's four
  context-cancelation exits published `Activity{Type: "done"}`;
  `observe()` folds any `done` as the terminal outcome, so the live
  state flipped `done` while the caller settled `aborted` — a registry/
  state divergence the one-settlement rule can never repair. Repair:
  honest `aborted` activity type end-to-end (orchestrator exits,
  `observe` folding, `captureTerminal` + resumed-path caption capture
  replacing the hardcoded "Completed", frontend typed `aborted` kind +
  dedicated store case replacing caption-string sniffing). Evidence:
  15/15 stressed abort-after-resume runs; deterministic regressions
  `TestAbortedRunStateAgreesWithOutcome`, `TestRunLiveObserveAbortedMarker`.
- Feature: Runtime Governor vertical slice (`internal/governor`) —
  ONE runtime policy authority fed by the EXISTING live monitor (same
  sampler, same cadence, same protection path; the monitor's cooperative
  critical protection unchanged). `ResourceState` (measured facts +
  explicit unknowns), sustained-duration + bounded rolling signals over
  the shipped four-level pressure vocabulary, `Envelope` with adjustment
  classes (`live`/`next-run`/`reload`), `AdmitHeavyweight`/`AdmitMemory`
  resident-budget admission (mapped size ≠ resident; unknown →
  conservative refusal), `SelfModel` with provenance. Policy only — no
  second sampler/scheduler/authority; executes nothing.
- Wiring: `Stack.StartGovernor()` on the existing poll path
  (`observeGovernor`); engine facts through the existing `llm.Backend
  Metrics` (read-only); active runs through the existing counter; CPU
  through an injected platform seam (Linux measured load average;
  Windows honestly unknown with CPU policy inert).
- Surface: `GET /api/governor` composing governor + sysinfo hardware
  (GPU/NPU labeled detection-only) + engine metrics + capabilities;
  System Centre `GovernorCard` (real values, named unknowns). API
  contract tests + frontend typecheck/lint/units/build green.
- Docs: README rewritten current + compact history; ARCHITECTURE.md
  rewritten around the v1.8 truth + compressed historical layers;
  ROADMAP.md rewritten capability-driven (v1.9+ explicitly FUTURE —
  NOT IMPLEMENTED); UPDATE.md top section = v1.8.0 with compressed
  records; agent.md = current handoff; REPORT.md marked unmistakably
  historical; worklog updated (this entry). Version identity 1.8.0 via
  the canonical gate (`release-version.mjs --check` green: package.json,
  package-lock, config.go, build/config.yml, SIGNATURE).

Verification (evidence classes; Linux x86-64, Go 1.26.0, Node 24):
- Deterministic unit: `go test ./internal/governor/ -count=3` green;
  new api regression suites green (`-count=5` targeted, family `-count=3`).
- Race: `go test ./internal/governor/ -race -count=3` green; api+agent
  full suites `-tags headless -count=2` green (176s + 40s).
- Integration: `/api/governor` contract; pause/resume/abort/edit family.
- Frontend: `npm run typecheck` (CI script), `oxlint` 0 warnings,
  `npm run test:units` green, production build + embedded sync green.
- Release: `node scripts/release-version.mjs --check` consistent at
  1.8.0; codename gate green on the tracked tree (and re-run against the
  clean-room drop at packaging).
- NOT executed here (stated honestly): physical-PC Windows runtime
  verification; native C++ build/ctest on this host (GTK/WebKit-free
  environment builds headless targets only); Browser E2E (Playwright)
  in this session.

Stage Summary:
- v1.8.0 complete on this revision: the actual CI failure is repaired at
  the root with regression coverage that makes stale cumulative-state
  reuse impossible; the abort path is honest end-to-end (flake
  eliminated); the Runtime Governor vertical slice is real, wired and
  exercised; docs are consolidated (current + history, no duplicated
  essays); version identity is exactly 1.8.0. Pre-v1.8 session records
  are summarized once below.

---
Task ID: 1 (pre-v1.8.0 record — summarized once)
Agent: v1.3.0 → v1.7.6 engineering sessions
Task: everything shipped before v1.8.0

Summary (compressed; the authoritative per-release detail lives in the
release tags, their tests, and the historical sections of UPDATE.md):
- Foundations (≤v1.2): sessions, streaming with sequence-stamped replay,
  tool registry, downloader, installer, health checks, environment
  centre, bounded authoritative run state, settlement edge.
- v1.3–v1.4: durable completion ordering (persist → summary → handoff →
  recall → rollover → terminal), idle-sentinel barrier, task state,
  cross-mode history, continuum rollover, repository index, custom
  tools, update rollback safety.
- v1.5–v1.6: multi-agent context, memory/recall tiers, Lab workspaces,
  engine-variant provisioning through the authoritative resolver,
  Vulkan asset validation on the Windows runner, clone + workspace
  surfaces, stable embedded-frontend asset contract.
- v1.7.0–v1.7.2: reliability passes (engine lifecycle, hub replay,
  stress surfaces), scheduler settlement contract, tool-registry
  snapshot isolation (the one-word-chat crash repair).
- v1.7.1: preflight gate (refuse before engine start), LiveMonitor with
  hysteresis + cooperative critical protection, context-exhaustion
  recovery (typed conditions, one bounded restart), native engine
  first-class backend, LICENSE.md consolidation.
- v1.7.4–v1.7.6: pause/edit/resume with durable checkpoints; transactional
  edits + journals + ten-window fault-injection recovery; restart
  recovery claiming the checkpoint revision; token-aware codename gate;
  generation-aware spec cache; the llama.cpp b10642/b11205 CLI contract
  verified against real `--help` fixtures.
