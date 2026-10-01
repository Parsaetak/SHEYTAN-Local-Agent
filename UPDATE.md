# UPDATE.md — v1.8.3 Release Notes & Maintenance Behavior

**Release:** `v1.8.3` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ c8867886` (`v1.8.2`) · **Date:** 2026-10-01
**Package:** `SHEYTAN-Local-Agent-v1.8.3-FINAL.zip` (complete repository
tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior.

## v1.8.3 changes

1. **P0 — the Linux CI session-delete failure is root-caused and repaired
   (run 36713108772 / job 109880449134, `e2e/sessions.spec.ts:88`:
   "Expected: < 4, Received: 4" after 15 s).** The root cause was
   established with an instrumented reproduction, not inference: the
   browser test sampled its count baseline BEFORE the asynchronous
   "New session" create landed in the sidebar, so the subsequent delete
   (which worked correctly — the deleted id is absent from the server's
   authoritative post-delete list) left the count flat against a baseline
   that did not yet include the in-flight create. The product never
   failed to delete and never resurrected anything; the measurement
   raced the app. The repaired test synchronizes on STATE (the created
   session appears; the count grows by one) before sampling, then
   asserts the SPECIFIC deleted id, the survivor identity set, the count,
   replacement activation, composer usability, and — after a full reload —
   that the deleted id is still absent. No sleeps, no timeout bumps, no
   weakened assertions.

2. **P0 — the startup session-list write goes through the ONE generation
   guard.** The v1.7.5 stale-response protection covered `refreshSessions`
   only; `initializeAgentOnce` applied its `GET /api/sessions` response
   without a ticket. Because the sidebar's "New session" button is
   actionable while the startup GET is on the wire, a session created in
   that window could be silently DROPPED from the sidebar by the stale
   startup list. The init now takes a `sessionListGuard` ticket before
   the GET; a response may only land while its ticket is current and the
   mode unchanged; superseded responses delegate list + selection
   re-resolution to `refreshSessions()`; the eager first-install create
   invalidates the guard like `store.createSession` does. Mutation-
   verified at two layers: both the store-level suite and a real-browser
   E2E fail against the unguarded v1.8.2 init and pass with the repair.

3. **P0 — a failed DELETE is surfaced honestly, and the created-session
   prepend is idempotent.** `deleteSession` now reports server-side
   failures through the store's error surface (the v1.8.2 sidebar turned
   them into invisible unhandled rejections) and never fakes success.
   `createSession` dedupes its prepend by id — a refresh landing between
   the create POST's dispatch and its response can no longer produce a
   duplicated sidebar row.

4. **P0 — deterministic session-delete coverage at three layers.**
   Store-level (`src/session-delete-regression.test.ts`: 11 scenarios
   over a scripted HTTP transport where response ordering is forced by
   causality — pending and persisted delete, replacement selection,
   delete-while-GET-in-flight, stale-GET non-resurrection, mode
   separation, honest duplicate-delete, post-delete fresh fetch, and the
   init race; plus the resolve hook that makes the real store testable
   outside the bundler), backend (`internal/sessions/
   delete_pending_test.go`), and browser (the strengthened delete test
   and the new init-race test, which holds the startup response until
   the create's POST response has reached the page — causality, not
   timers).

5. **P1 — `ROADMAP.md` repaired.** The roadmap now states the artifact
   truth (`changelog.md` is the sole release-history artifact; README is
   current-only; this file is current release/maintenance evidence) and
   adds the evidence-ranked Performance, Reliability & Scale Backlog
   with explicit D/I/H labels. Supplied external report content was
   treated as research input: unmeasured performance figures are
   hypotheses, incompatible architecture prescriptions (runtime
   rewrites, mandatory external stores, assumed engine features) are
   rejected, and no speculative claim became a product fact.

v1.8.2's streaming, live memory-evidence and self-model behavior is
preserved unchanged — the full browser suite (including every
live-stream test) passes against this build.

## Maintenance / update / rollback behavior (current)

* The maintenance gate (identity manifest + committed state) still owns
  engine updates; two-boot idempotency and the corruption matrix are
  unchanged (deterministic, fault-injected).
* A rolled-back engine transaction leaves the RECORDED tag agreeing with
  the restored package (re-recorded from the restored manifest; no
  invented identity). The commit remains the authoritative identity
  record — state, manifest, binary hash all agree after every successful
  update, and the log sequence names which build is serving and which is
  staged.
* The update path still resolves engine variants through the ONE
  authoritative resolver (pinned tag first, else newest release containing
  the asset); unsupported variants are REFUSED, never silently CPU-fallen.
* Update rollback safety, zipsafe installation and effective-tag rules are
  unchanged from v1.7.4–v1.7.5 (plus the v1.8.1 rollback identity repair).
* The model capability cache is in-memory only (bounded, identity-keyed);
  restarting the app re-reads each model's card once. Nothing to migrate.
* The Runtime Governor adds NO persistence, NO new scheduler and NO new
  updater: it is a policy read-model over existing telemetry. Restarting
  the app re-measures; nothing to migrate.
* v1.8.3 adds no data migration of any kind: the session store's on-disk
  format, the index and the sidecars are byte-compatible with v1.8.2.

## Evidence-truth statements (standing)

* Deterministic unit / race / integration / E2E / CI / real-engine probe /
  real-host runtime are DISTINCT evidence classes and are never conflated.
* v1.8.3 session-delete evidence: the store-level suite forces the exact
  CI-latency interleavings by causality (held responses released only
  after the mutation completes), the Go suite pins the pending/persisted/
  repeat-delete contracts, and the browser suite proves the same through
  the real stack. Mutation checks (reverting the guard) make both the
  store-level and browser tests fail — the coverage detects the defect
  class it was written for.
* Build/typecheck ≠ runtime proof; CI ≠ physical-host runtime proof;
  detection ≠ execution. The physical-Windows acceptance walkthrough
  (launch, create/delete pending and persisted sessions, ordinary chat
  with live streaming, memory evidence, capability self-description,
  reload no-resurrection) remains the user's own acceptance step — no
  physical-PC claim is made by CI or by this release.

---

# Historical records (compressed — authoritative detail lives in the tags, their tests, and `changelog.md`)

## v1.8.2 record

The "text visible only after Stop" live-rendering defect repaired by the
dual-boundary flush (event-loop task + animation frame, one coalescing
latch); factual no-reasoning-stream state; backend-truth memory evidence
composed at the injection site; the deterministic capability self-model;
the (path,size,mtime) model-card cache; central log redaction.

## v1.8.1 record

The real-Windows local-generation crash repaired at the root (Gemma-class
projection-tensor quantization mismatch: verified before download,
refused with the exact mismatch named); engine-build identity contract.

## v1.8.0 record

Runtime Governor: resource state, envelopes, admission, hysteresis,
self-model, `/api/governor`; cooperative protection unchanged.

## v1.7.6 record

Continuum (chapter rollover) + vision-projector honesty surfaces.

## v1.7.5 record

Session-list generation guard (refreshSessions consumer), run-control
E2E, stale-run protection hardening.

## v1.7.4 record

Pause/edit/resume (durable checkpoints, revision conflicts), live
streaming fast path foundations.

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
