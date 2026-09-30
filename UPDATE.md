# UPDATE.md — v1.8.1 Release Notes & Maintenance Behavior

**Release:** `v1.8.1` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ e5a12c0` (`v1.8.0`) · **Date:** 2026-09-30
**Package:** `SHEYTAN-Local-Agent-v1.8.1-FINAL.zip` (complete repository
tree)

## v1.8.1 changes

1. **P0 — the real-Windows local-generation crash (`hi` on a small local
   Gemma model), repaired at the root.** Evidence baseline: a user's
   v1.8.0 Windows session (CPU backend, `gemma-4-E2B-it-Q4_K_M.gguf`,
   llama.cpp b11261, preflight `compatible=true`) logged
   `recovered panic: runtime error: invalid memory address or nil pointer
   dereference` right after `task classified: kind=chat complexity=8`,
   settling as an error in ~57 ms with no request-sent/response-header/
   first-byte evidence. Root cause (reproduced deterministically before
   the fix): Gemma-class GGUFs carry 262,144-entry tokenizer blocks whose
   metadata exceeds the model-card parser's 8 MiB read bound →
   `ReadModelCard` fails at the `LimitReader` →
   `ResolveModelCapabilities` returns nil → the orchestrator's
   `resolveEffectiveContext` dereferenced `caps.TokenizerFamily` between
   `task classified` and `tier selected`. Two repairs, both verified:
   (a) the nil-card path now honors the documented caps contract (the
   configured context + the conservative heuristic estimator — exactly
   the remote/unresolved-model fallback); (b) the GGUF metadata read
   bound is 32 MiB (`ggufMetadataReadLimit`), so real Gemma-class cards
   parse again, restoring the model-aware context clamp and the
   family-tuned estimator. Same class, second site:
   `ResolveModelCapabilities` itself guarded `cfg` for vision but
   dereferenced `cfg.LLM` for recommendations — now uniformly nil-safe.
   The Gemma lineage lists also recognize `gemma3n`/`gemma4`.
   Evidence: `TestResolveEffectiveContextSurvivesUnreadableCard` (panics
   on the pre-fix code), `TestGemmaClassCardParsesUnderRaisedBound`,
   `TestResolveEffectiveContextGemmaClassEndToEnd`, plus the first
   LOCAL-provider run-level E2E tests in the api package —
   `TestLocalGemmaHiChatCompletes` (fake llama-server spawned as a real
   subprocess: engine request evidence, streamed response frames before
   `done`, one persisted reply, one settlement, a second ordinary chat)
   and `TestLocalChatSurvivesUnreadableModelCard` (verified to fail on
   the pre-fix code with the exact settled-as-error signature).

2. **P0 — low first-visible latency: streamed content now crosses ONE
   render-frame boundary, not two.** The store previously routed every
   activity frame through `queueActivity → rAF (activity batch) →
   handleConversationEvent → queueStreamingContent → rAF (streaming
   flush) → React render`. The new single-frame fast path
   (`src/stream-fast-path.ts`, wired at the WebSocket receive boundary)
   folds stream-critical events (`response`/`reasoning` — canonical
   `assistant_delta`/`thinking_delta`) into the streaming accumulator the
   moment the socket delivers them; the event still joins the timeline
   batch, and a self-draining ledger makes the batch skip its
   conversation processing (never processed twice). Preserved unchanged:
   cumulative-snapshot replace semantics, replay/reconnect idempotence,
   run-sequence/stale-run protection, lifecycle ordering, and the
   SYNCHRONOUS done/error/abort flush (no second frame for terminal
   events). No artificial delays, no per-token renders. Evidence: 12
   deterministic tests (`src/stream-fast-path.test.ts`) driven by a fake
   `requestAnimationFrame` controller — no sleeps.

3. **P1 — engine rollback restores the recorded identity.** During
   startup verification (between the package swap and the commit)
   `ensureBinary` may stamp the bundled-default tag as the last-resort
   identity of a tagless, manifest-less binary — the exact transient in
   the user's log (`recording the bundled default tag b10642`). If
   verification then fails and the previous package is rolled back, that
   stamp survived in `installed.json` while the restored manifest
   described the actually-serving engine; since
   `EffectiveInstalledEngineTag` is state-first, the next "update
   required" comparison and the UI engine tag would read the wrong
   build. `Rollback()` now re-records the tag from the restored manifest
   (conservatively: no invented identity when the manifest has none).
   Evidence: `TestRollbackRestoresRecordedIdentityFromManifest` (fails on
   the pre-fix code at exactly `b10642`), `TestCommitRemainsTheAuthoritativeIdentity`, `TestRollbackLeavesStateUntouchedWhenManifestHasNoTag`.

4. **Audited, no change required.** The legacy AppData root migration
   logged by the user (`detected=1 merged=1 recovered=0 collisions=4
   removed=1 reloadConfig=false`) IS the intended contract — deterministic
   newer-wins collisions, one authoritative root afterward, idempotent
   next boot — and is pinned by the existing migration test family.
   GPU/AUTO selecting CPU with `numGPU=0` and no measured accelerator
   evidence remains the honest evidence-ladder outcome. The Runtime
   Governor was not expanded; the v1.8.1 changes touch no resource-state
   collection, pressure hysteresis, admission or protection path.

## v1.8.0 changes (historical, compact)

1. **P0 — the Windows pause→resume→pause synchronization defect, repaired
   at the root.** `TestPauseResumePauseAgainThenResumeCompletes` failed on
   the Windows runner (Actions run `36553559366`: `run settled "done"
   (error "") while waiting for phase "paused"`). Root cause: the test
   reused `waitForRunResponse` after a resume — its condition
   (`LatestResponse != ""`) is satisfied by the STALE pre-resume cumulative
   snapshot, so the second pause was issued without proof the resumed
   generation was live, and a slow runner settled the run `done` first.
   Production behavior was not weakened; the synchronization was repaired
   with an authoritative-evidence contract: `resumedGenerationEvidence`
   requires a strictly newer run sequence AND a changed cumulative
   response/reasoning snapshot (a status/task event bumps the sequence
   without being generation evidence; the pre-resume text is exactly the
   stale state rejected). `waitForResumedGeneration` polls that contract
   and fails fast on a settled run. Evidence: deterministic unit contract
   (`TestResumedGenerationEvidenceContract`), integration replay of the
   exact CI scenario (`TestPauseResumeRequiresNewGenerationEvidence`),
   targeted `-count` stress plus the full pause/resume family — all green,
   repeatedly.

2. **P0 — honest abort marker end-to-end (latent race repaired).** Found
   during the v1.8 audit: the orchestrator's context-cancelation exits
   published `Activity{Type: "done"}` with an abort caption. `observe()`
   folds any `done` as the terminal outcome — so the live state flipped to
   `done` while the caller settled `aborted` (registry record first), a
   divergence `settleTerminal` can never override (one-settlement rule),
   and the source of a ~17% flake in `TestAbortReachesResumedGeneration`
   on Linux (the poll loop observed the wrong live state depending on the
   settle-tail timing). Repair: the four cancelation exits now publish
   `Type: "aborted"`; `observe()` folds it honestly
   (`terminalOutcome=aborted`, phase `aborted`); `captureTerminal` and the
   resumed path capture the real terminal caption (the resumed path
   previously hardcoded `"Completed"`); the frontend consumes the typed
   `aborted` marker (`run-events.ts` kind + dedicated store case — no more
   caption-string sniffing). Evidence: 15/15 stressed abort-after-resume
   runs (previously ~1-in-6 flaky at BASELINE); new deterministic
   regressions `TestAbortedRunStateAgreesWithOutcome` and
   `TestRunLiveObserveAbortedMarker`.

3. **Runtime Governor — Adaptive Runtime Intelligence vertical slice.**
   `internal/governor` is the ONE runtime policy authority, fed by the
   EXISTING live-pressure monitor (one sampler, one cadence, one protection
   path — the monitor's cooperative critical protection is unchanged and
   its owner untouched). Delivered: `ResourceState` (measured RAM,
   process/engine RSS where measurable, active runs from the run
   authority, CPU where the platform seam can measure, explicit unknowns);
   sustained-duration tracking and bounded rolling signals (EWMA) over the
   shipped four-level pressure vocabulary; `Envelope` with admission,
   background/context/tool-concurrency posture, resident admission budget
   and honest adjustment classes (`live` / `next-run` / `reload`);
   `AdmitHeavyweight`/`AdmitMemory` with resident-budget arithmetic
   (mapped file size is never resident size; missing estimates and unknown
   RAM refuse conservatively); `SelfModel` with provenance. Policy only —
   the Governor executes nothing. Evidence: deterministic unit matrix
   (`internal/governor/governor_test.go`, also run under `-race`).

4. **System-health surface.** `GET /api/governor` composes one read model
   from the EXISTING authorities (governor policy state; sysinfo hardware
   with GPU/NPU labeled detection-only; engine health through the existing
   `Metrics`; construction-time capabilities). The System Centre renders a
   Runtime Governor card — real values, named unknowns, no decorative
   telemetry. Evidence: API contract tests
   (`internal/api/governor_test.go`), frontend typecheck + unit suites +
   production build.

5. **Documentation truth.** README rewritten as the current product
   document (v1.3–v1.7 essays compressed to a compact history);
   ARCHITECTURE.md rewritten around the v1.8 truth (historical layers
   compressed); ROADMAP.md rewritten capability-driven from the 2026-09
   R&D (v1.9+ explicitly FUTURE — NOT IMPLEMENTED); agent.md is the
   current handoff; worklog updated to v1.8.0; REPORT.md marked
   unmistakably historical. Version identity synchronized to 1.8.0 through
   the canonical gate (`node scripts/release-version.mjs --check` green).

## Maintenance / update / rollback behavior (current)

* The maintenance gate (identity manifest + committed state) still owns
  engine updates; two-boot idempotency and the corruption matrix are
  unchanged (deterministic, fault-injected).
* v1.8.1: a rolled-back engine transaction now leaves the RECORDED tag
  agreeing with the restored package (re-recorded from the restored
  manifest; no invented identity). The commit remains the authoritative
  identity record — state, manifest, binary hash all agree after every
  successful update.
* The update path still resolves engine variants through the ONE
  authoritative resolver (pinned tag first, else newest release containing
  the asset); unsupported variants are REFUSED, never silently CPU-fallen.
* Update rollback safety, zipsafe installation and effective-tag rules are
  unchanged from v1.7.4–v1.7.5 (plus the v1.8.1 rollback identity repair).
* The Runtime Governor adds NO persistence, NO new scheduler and NO new
  updater: it is a policy read-model over existing telemetry. Restarting
  the app re-measures; nothing to migrate.

## Evidence-truth statements (standing)

* Deterministic unit / race / integration / E2E / CI / real-engine probe /
  real-host runtime are DISTINCT evidence classes and are never conflated.
* v1.8.1 crash repair evidence: the panic was reproduced deterministically
  from the user's log signature BEFORE the fix, and every regression here
  was verified to fail against the pre-fix code. The end-to-end `hi` runs
  use a REAL local-engine subprocess (the api package's first LOCAL-path
  run-level tests); a physical Windows host run remains the user's own
  acceptance step — no physical-PC claim is made by CI.
* Build/typecheck ≠ runtime proof; CI ≠ physical-host runtime proof;
  detection ≠ execution.

---

# Historical records (compressed — authoritative detail lives in the tags and their tests)

## v1.7.6 record

Edit-transaction journals (atomic, integrity-hashed) + startup recovery
converging interrupted edits from durable evidence across ten
crash/failure windows (deterministic fault-injection matrix); corrupt
journals fail closed (quarantined); restart-recovered runs claim the
checkpoint revision; `/api/run/paused` excludes journals/temp files;
token-aware codename gate (every spelling fails, legitimate compound
identifiers pass); generation-aware spec cache (registry generation
carried in cache entries).

## v1.7.5 record

Transactional pause/edit/resume: CAS validation without mutation,
transcript replace, checkpoint commit, transcript rollback on commit
failure, publication strictly last; one control mutex serializing
edit/resume/stop; resume handler owns the fresh generation context (abort
reaches the resuming window); stale session-list responses cannot
resurrect deleted sessions (monotonic generation guard); Windows migration
"restart" test realism; strict numeric engine-variant gates.

## v1.7.4 record

The pause/edit/resume state machine (RUNNING → PAUSING → PAUSED → RESUMING
→ terminal), durable checkpoints, WS snapshot/replay continuity across
pause/resume, double-pause idempotency, stale-revision 409s, stop-after-
pause semantics, one-run-per-session replacement consuming checkpoints.

## v1.7.0–v1.7.3 record

Scheduler RunNow settlement contract (close = persistence + bookkeeping
complete); recovery/handoff durability; engine-lifecycle and stress
hardening.

## v1.7.1 record

Preflight gate: one report over existing authorities, hard
incompatibilities refuse runs BEFORE engine start; LiveMonitor with
hysteresis and synchronous cooperative critical protection (active run
contexts canceled — nothing killed); context-exhaustion recovery (typed
conditions only, exactly one bounded restart); native engine first-class
backend; license/doc cleanup.

## v1.6.x record

Engine-variant provisioning through the authoritative resolver with
Windows-runner asset validation (HEAD-checked reachability ≠ runtime
execution — labeled then and now); stable embedded-frontend asset
contract; custom tools surface; workflow output-name repair.

## v1.5 and earlier (compressed)

Multi-agent context, memory/recall tiers, durable summaries, cross-mode
history, task state, agent.md handoff, continuum rollover, repository
index, Lab workspaces, downloader/installer foundations, environment
centre, streaming replay contracts — each with the deterministic and
integration evidence that shipped in its release.
