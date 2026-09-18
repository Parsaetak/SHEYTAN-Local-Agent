# UPDATE.md — v1.2.7 SHEYTAN-LA Run-Transport Terminal-State Repair, CI/Test Stabilisation, Release Consistency

**Release:** `v1.2.7` (codename Zeta) · **Base:** `main @ e759189` (`v1.2.6`)
**Date:** 2026-09-19
**Package:** `SHEYTAN-Local-Agent-v1.2.7-UPDATE.zip`

This update fixes the failed CI run 35367243405 at its root and closes
the run-lifecycle audit that failure exposed. The failing test was
`TestStaleRunEventsFilteredByServer` ("expected idle, got
run_snapshot") — a real transport bug, not a flaky test: the activity
WebSocket attach path treated the run-registry map membership as "a run
is active", while a run that has already settled but has not yet been
deleted from the registry (the deferred cleanup first releases the run
budget and the memory-manager tracking) is authoritatively TERMINAL. A
socket attaching inside that window received a stale terminal
`run_snapshot` and then — because the hub closed without any forwardable
post-snapshot event — parked on `clientGone` forever, with no terminal
marker at all. The repair makes the authoritative `runLive` state the
only lifecycle authority, orders the outcome registry record before the
terminal flip, and adds a terminal-recovery fallthrough. Three
regression tests pin the contracts. No architecture was rewritten, no
queue system was added, no runtime changed.

## 0. Execution method (read this first)

- The failure was REPRODUCED before any edit: the test passes in
  isolation (`-run TestStaleRunEventsFilteredByServer`) and fails the
  full package run roughly 1 in 5 times (shared-state ordering race,
  window widened by `MemMgr.TrackRunEnd` inside the deferred cleanup).
- The root cause was fixed in the transport, not in the test. The test
  was not touched; three new tests were ADDED alongside it.
- Everything below is measured from the actual code and executed
  commands — nothing is claimed from compilation success alone.

## 1. ROOT CAUSE — run transport terminal-state mismatch

The fast path of `handleActivityWS` (internal/api/server.go):

```go
s.runsMu.Lock()
rs, ok := s.runs[sessionID]
s.runsMu.Unlock()

if ok { /* subscribe, send run_snapshot, forward live events */ }
```

map membership alone decided "a run is active". The run goroutine's
exit path is: `settle()` (authoritative terminal flip + outcome record)
→ deferred `budgetCancel()` → `MemMgr.TrackRunEnd()` → map delete →
`hub.close()` → `cancel()`. Between the terminal flip and the delete the
entry exists while `live.snapshot().Running == false`. Attaching there
produced the exact CI failure. Worse: after the stale snapshot, the
hub's closed-channel drain yielded nothing (`served == false`) and the
old code blocked on `<-clientGone` — the socket never received ANY
terminal marker.

The fix (three coordinated edits, one lifecycle authority):

1. **Authoritative lifecycle gate** — after the map lookup:

   ```go
   if ok && rs != nil && rs.live != nil && !rs.live.snapshot().Running {
       ok = false
   }
   ```

   A terminal entry falls through to the standby path, whose idle
   sentinel carries the recorded `lastRun` outcome.

2. **Outcome-record ordering** — inside `settle()`, the outcome is
   recorded in the bounded registry BEFORE `live.settleTerminal()`
   flips the authoritative state. Invariant for every observer:
   terminal-visible ⇒ outcome recorded (happens-before edge through
   `settleTerminal`'s mutex). The idle sentinel therefore always has
   the `lastRun` block in the gate's fallthrough.

3. **Terminal-recovery fallthrough** — the post-drain `served == false`
   branch no longer parks on `<-clientGone`. The socket falls through
   to the idle sentinel and the standby loop, which re-checks the runs
   map and attaches to a replacement run the moment one starts. The
   write-failure clientGone check (`served == true` path) is preserved.

## 2. Run-lifecycle audit results (boundaries re-verified)

- **Identity** — one runId minted per run, stamped by the single
  `publish()` wrapper; the replay filter drops events of other runs
  (`ev.RunID != snap.RunID`) and already-folded sequences
  (`ev.Seq <= snap.Sequence`). Unchanged and re-verified.
- **State monotonicity** — `runLive.observe` and `settleTerminal` only
  ever move a run toward terminal (`terminalOutcome` is exactly-once);
  a terminal run can never become active again. The gate now ENFORCES
  this at the attach boundary too.
- **Replacement** — the newer run's registration cancels+closes the
  older run and replaces its entry; the older goroutine's cleanup only
  deletes the entry it still owns (`current.hub == hub`). Unchanged.
- **Abort** — `POST /api/abort` and the WS `abort` action cancel the
  current entry's context; the run settles from real signals
  (`ctx.Err()` → "aborted"). New regression test proves a fresh attach
  after an abort receives the idle sentinel with the recorded outcome.
- **Cleanup** — `hub.close()` is idempotent (double-close safe);
  subscribers receive closed channels, never send-on-closed; race
  detector runs over the transport, scheduler, runtime, continuum and
  sessions packages are clean.
- **WebSocket / reconnect** — `attached` ack first, `run_snapshot` on
  every live attach, gapless + duplicate-free replay; the frontend was
  re-audited against the fixed server (its `idle` dispatch is guarded,
  its `lastRun` recovery is authoritative) and needed no changes.

## 3. What is preserved

Everything from v1.2.6: the authoritative run transport (runstate.go,
single publisher, seq/runId filtering), the deterministic `attached`
ack, the bounded outcome registry, the measured run timeline, the
non-blocking startup, the evidence-based accelerator resolution, the
adaptive context tiers, the download manager. The only behavioural
changes are the three repairs above.

## 4. Verification performed (all executed, all clean)

```text
go test ./internal/... -tags headless -count=1     PASS (all packages)
go test -tags headless ./... -run Test -count=1    PASS (all packages)
go vet -tags headless ./...                        PASS
go test ./internal/api -tags headless -race        PASS (incl. 15× loop of the
                                                    previously flaky suite)
go test -race (scheduler/runtime/continuum/sessions) PASS
npm ci                                             PASS (0 vulnerabilities)
npm run typecheck                                  PASS
npm run lint                                       PASS (0 warnings, 0 errors)
npm run test:units                                 PASS (39/39)
npm run build                                      PASS (embedded web/static synced)
cmake + ctest native/engine                        PASS (12/12)
stress suite ./scripts/stress-main stress          PASS (47/47, 0 hangs, 0 crashes)
node scripts/release-version.mjs --check           PASS (all surfaces 1.2.7)
```

## 5. Queue status (read literally)

Durable, guaranteed request queuing is NOT part of this release and is
not claimed anywhere. The current contract: one active run per session;
a newer `POST /api/run` cancels and replaces the active run by design;
invalid requests are rejected; requests lost to a process crash are
gone (sessions persist, queued intents do not). The durable queue
(persistent jobId, ACCEPTED → QUEUED → WAITING_FOR_RESOURCES → RUNNING
→ COMPLETED/FAILED/CANCELED with retry states, FIFO/fair per-session
scheduling, bounded concurrency, crash recovery, idempotent execution,
engine/resource gating, queue-state replay) remains documented design
intent in `README.md` ("Development direction") and `ARCHITECTURE.md`
(Part II).

## 6. Known limitations (unchanged, honest)

- The v1.2.6 honest limitation stands: implemented and tested on Linux;
  Windows-only paths compile (GOOS=windows gate) and their parsers are
  fixture-tested, but real-hardware Windows validation was NOT
  performed here.
- The native engine serves only the llama-architecture path it
  documents; llama.cpp remains the default and the fallback.
- GPU/NPU acceleration requires measured engine/runtime evidence;
  presence of a Vulkan DLL or an NPU device alone never selects a
  non-CPU execution target.

## 7. Apply procedure

1. Back up or commit the current working tree.
2. Extract `SHEYTAN-Local-Agent-v1.2.7-UPDATE.zip`.
3. Copy the extracted `SHEYTAN-Local-Agent-v1.2.7-UPDATE/` tree over
   the repository root (it contains the complete updated project state:
   `internal/`, `src/`, `scripts/`, `packaging/`, `build/`, `.github/`,
   docs and all package metadata — no `.git`, no `node_modules`).
4. Verify the release identity: `node scripts/release-version.mjs --check`.
5. Build and test: `npm ci && npm run typecheck && npm run lint && npm
   run build`, then `go test ./internal/... -tags headless -count=1`,
   `go test -tags headless ./... -run Test -count=1`,
   `go vet -tags headless ./...`, and optionally the native engine
   (`cmake -S native/engine -B native/engine/build && cmake --build
   native/engine/build && ctest --test-dir native/engine/build`) and
   the stress suite (`go run ./scripts/stress-main stress`).
6. Commit and push to GitHub as usual — the ZIP is the complete update;
   nothing else is required.
