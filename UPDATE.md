# UPDATE.md — v1.3.3 Deep Repair: Settlement Determinism, Native Windows Build, Version-Only Release Identity

**Release:** `v1.3.3` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ dfce793` (`v1.3.2`) · **Date:** 2026-09-21
**Package:** `SHEYTAN-Local-Agent-v1.3.3-UPDATE.zip`
**ROADMAP.md:** byte-identical to the locked baseline (git blob SHA-1
`c7e2c1720eb5e97bd932c0d76100b8719193e650`) — verified before and after
all work.

This release root-fixes the three failure classes of Actions run
`35552680611` and hardens the release identity permanently:

1. the Linux settlement race (summary completion used as a proxy for
   complete agent settlement while production writes the summary BEFORE
   the agent.md handoff);
2. the Windows native-engine compilation failure (transitive includes
   and a `windows.h` min/max macro collision in `hardware.cpp`);
3. the release-title identity drift (the GitHub Release title derived
   from the tag with a product prefix instead of the plain canonical
   version).

No working architecture was changed.

---

## S1. Deterministic settlement barrier (ROOT FIX, TESTED)

### The failure

`TestAgentRunSettlesWithSummaryAndHandoff` failed on CI with "agent.md
handoff missing". The test barrier (`waitForSummarySettled`) returned as
soon as the rolling summary sidecar reached version >= 1 — but production
writes the summary (durable step 1) BEFORE the agent.md handoff (durable
step 2), recall indexing, the continuum rollover and only then records
the terminal outcome. A test that stops waiting at the summary observes
a run whose settle-tail is still in flight: on a loaded CI runner the
poll can land in the summary→handoff window and read a missing agent.md.
A real race, not a flaky assertion.

### The fix

Settlement now waits on the TERMINAL OUTCOME itself:

- `waitForRunSettled(t, srv, sessionID)` polls the bounded outcome
  registry. The v1.2.9 durable-completion ordering guarantees the
  terminal outcome is recorded only AFTER every required durable
  artifact is on disk — so observing the terminal outcome IS observing
  complete settlement. Summary completion is never a proxy again.
- `waitForRunSettledFor(t, srv, sessionID, runID)` keys the barrier on
  the EXACT run id — repeated settlement (sequential runs in one
  session) cannot cross the barrier on a stale terminal record.
- `waitForRunOutcome(t, srv, sessionID, want...)` pins WHICH terminal
  state a run settles into and fails fast on a contradicting record.
- Every settlement-waiting test in the package was migrated
  (runregistry, runtransport, run_settlement, run_crossmode, run_task);
  `newRemoteServer` now returns the `*Server` handle so tests can poll
  the registry through the same observable production writes.

### Regression coverage

- `TestSettlementBarrierWaitsForBothSummaryAndHandoff`: three sequential
  agent runs in ONE session; after every barrier crossing BOTH required
  artifacts (summary version >= N, agent.md with the handoff section)
  are verified on disk immediately — no post-barrier grace, no sleeps.
- The original test now verifies both summary AND agent.md after the
  barrier.
- 20 consecutive iterations of the four settlement tests under
  `-race`: PASS.

## S2. Mandatory-handoff honesty (ROOT FIX, TESTED)

### The failure mode

Production could write the summary, fail the agent.md handoff, and still
`settle(resultOutcome, ...)` with `resultOutcome == "done"` — a falsely
successful terminal result while the next agent starts without its
handoff. This violated the documented durable-completion invariant.

### The fix

For a completed Agent run the agent.md handoff is REQUIRED durable
state, not best-effort decoration:

- the write is attempted for EVERY completed agent run (`resultOutcome
  == "done"` + agent mode) — a nil task state is reported by
  `writeAgentHandoff` as a real failure instead of silently skipping
  the mandatory artifact;
- a write failure demotes the terminal outcome to `"error"` — the same
  honest class as a reply-persistence failure — with the concrete cause
  in the terminal caption; the post-terminal error activity still
  surfaces the failure to the live stream; the reply and the summary
  are preserved (the run's content is not lost — only its terminal
  status is honest);
- the successful ordering (summary → handoff → recall → continuum →
  terminal publication) is unchanged.

### Regression coverage

- `TestMandatoryHandoffFailureCannotMasqueradeAsDone`: deterministic
  failure injection (workspace root is a regular FILE →
  `os.CreateTemp` fails with ENOTDIR on every platform — no timing, no
  permission lottery); asserts outcome == "error", caption carries the
  concrete cause, reply persisted, summary present, handoff genuinely
  absent.
- `TestHandoffFailureSurfacesErrorActivity`: a connected activity
  WebSocket actually RECEIVES the live error frame with the concrete
  failure; the session keeps the assistant reply.

## S3. Windows native-engine compilation (ROOT FIX, VERIFIED)

### The failure

`native/engine/src/hardware.cpp` failed to compile for Windows:
`std::vector` used with no `<vector>` include (transitive on some
toolchains only), `__cpuid` with no `<intrin.h>`, and `std::min` exposed
to the `windows.h` min/max macro collision (`WIN32_LEAN_AND_MEAN` does
NOT suppress the macros; a function-like macro expands even after `::`).

### The fix

- every header the translation unit actually uses is included
  EXPLICITLY: `<vector>`, `<cstdlib>` (strtod/strtoll), `<intrin.h>`
  (`__cpuid`, Windows-only);
- `NOMINMAX` + `WIN32_LEAN_AND_MEAN`, both idempotently guarded, before
  `<windows.h>` (the pattern `gguf.cpp` already carried);
- the `std::min` call is parenthesized — `(std::min)` — immune to any
  macro a consumer's include order may still define;
- the three test files that include `windows.h` (test_gguf, test_model,
  test_host) carry the same guards;
- Linux/macOS behavior unchanged (verified: the Linux build is
  byte-identical in behavior; 12/12 CTest pass).

### Verification performed on this host

- REPRODUCED: the pre-fix `hardware.cpp` compiled against a real
  Windows SDK (MinGW-w64, `x86_64-windows-gnu`) fails with exactly the
  CI error class (`no member named 'vector' in namespace 'std'`;
  `use of undeclared identifier '__cpuid'`).
- FIXED: the patched file compiles clean.
- The min/max collision mode was proven token-level (the macro shape
  MSVC's windows.h defines breaks unparenthesized `std::min`; the
  parenthesized form compiles and runs) — MSVC itself runs on CI.
- CLEAN full-engine Windows cross-build: `shtn-engine-host.exe` + all
  12 CTest executables build as PE32+ x86-64 with
  `-Wall -Wextra -Wpedantic` and ZERO project warnings (the 8 pre-existing
  warnings — 1 production in sampler.cpp, 7 in tests — are eliminated).
- Linux clean-room: `rm -rf build && cmake --fresh && cmake --build &&
  ctest` → 12/12 PASS.
- Real application-path execution: the complete Go↔C++ host suite
  (lifecycle, handshake, health, hardware, metrics, model lifecycle,
  missing-model path, orphan prevention, stop-during-generation,
  tokenizer/KV/scheduler, end-to-end generation, cancellation) passes
  against the real host binary — 10 consecutive iterations.

## S4. Native engine deep audit (HARDENING, TESTED)

The complete path (Go runtime → engine selection → native host
discovery → process start → handshake → model load → generation →
streaming → cancellation → shutdown → restart → cleanup) was audited.
The architecture is sound (bounded restarts with backoff, fail-closed
handshake, teardown paths, no silent fallback). One real defect fixed:

- the IPC read loop could BLOCK forever dispatching an event frame to an
  ABANDONED stream whose 256-frame buffer was full (a straggler burst
  larger than the buffer wedges the loop ahead of the cancelled final
  frame — a wedged engine requiring restart). Abandoned streams now
  drop-if-full instead of blocking; live consumers keep full
  backpressure semantics.

Quality cleanup: 8 compiler warnings eliminated (unused `kept` counter
in `sampler.cpp`; dead variables/captures in test_forward/test_model/
test_host/test_generate; the test_generate rms-eps fixture now writes a
real GGUF F32 (type 6) value instead of a float truncated to u32 0).

## S5. Version-only release identity (PERMANENT, TESTED)

From v1.3.3 onward the release identity is VERSION ONLY:

- tag: `v1.3.3`; release title: `1.3.3` — the plain canonical version,
  never the tag, never a product prefix, never a codename, never a
  suffix;
- the release job verifies the pushed tag equals `v${APP_VERSION}`
  before anything is published, and re-verifies the PUBLISHED release
  carries both the exact tag AND the exact version-only title;
- the workflow contract structurally enforces the identity: REQUIRED
  fragments (the version-only title spelling, the tag-equals-version
  gate, the tag-from-ref publication) and BANNED fragments (the
  pre-v1.3.3 product+tag title, any `Zeta` codename fragment,
  `APP_VERSION_FULL`);
- the canonical version 1.3.3 is synchronized across package.json →
  internal/config/config.go (`AppVersion = "1.3.3"`) → build/config.yml
  (`productVersion: "1.3.3"`) → SIGNATURE (`SHEYTAN-Local-Agent v1.3.3`)
  by `release-version.mjs` (sync mode verified, `--check` passes);
- stale identity assumptions corrected: ARCHITECTURE.md and README rows
  claiming the `APP_VERSION_FULL` alias still exists now document its
  permanent removal. No `-Z` / `-Zeta` / codename / release-suffix
  reference survives in any active surface.

Artifact filenames stay versioned normally
(`SHEYTAN-LA-v1.3.3-windows-x64.zip`,
`SHEYTAN-LA-v1.3.3-windows-x64-installer.exe`,
`SHEYTAN-Local-Agent-Linux-x64-v1.3.3.zip`) — the release TITLE is the
only identity surface that is the bare version.

## S6. Verification performed (this release, this host)

- **Go:** `gofmt -l .` clean; `go vet -tags headless ./internal/...
  ./cmd/...` clean; `go test -tags headless -count=1 ./internal/...` →
  46/46 packages PASS (including the real C++ host integration suite,
  19.6 s); race gate on the concurrency-heavy packages
  (api/agent/sessions/contextplan/histref/runtime) PASS; settlement
  tests 20x under `-race` PASS; native lifecycle tests 10x PASS.
- **Frontend:** `npm ci`; `npm run typecheck` clean; `npm run lint`
  0/0; `npm run test:units` 78/78; `npm run test:release` 28/28;
  `npm run build` + `sync:web` OK; `npm run verify:web -- --dist`
  satisfied.
- **Native Linux:** clean-room CMake (`--fresh`) configure + build +
  ctest → 12/12 PASS.
- **Native Windows:** full-engine cross-build (host + 12 test
  executables) as PE32+ with zero project warnings; the MSVC + Windows
  CTest execution is owned by the GitHub Windows runner (the pipeline
  builds, ctests and stages the host before the Go integration suites
  run — unchanged from v1.3.2 and now compilable).
- **Release:** `node scripts/release-version.mjs` (sync) repaired all
  three derived surfaces to 1.3.3; `--check` passes; `--env` emits
  exactly `APP_VERSION=1.3.3`; the 28-test regression suite (7 new
  version-only identity tests) passes, including a live audit of the
  real workflow file.
- **Repository:** no `APP_VERSION_FULL` reference in any active surface
  (the release script's own historical comment and the removal tests
  intentionally document the dead alias); ROADMAP.md git blob SHA-1
  `c7e2c1720eb5e97bd932c0d76100b8719193e650` unchanged before and after.

## S7. Environmental limits (honest scope)

- The Wails desktop shell needs GTK4/WebKitGTK-6.0 development packages
  (not installable on this host) — the headless build tag is the
  documented verification path and is what CI runs in the audit job.
- The Windows MSVC build, NSIS installer and GitHub Release publication
  execute on the GitHub runners; on this host the Windows native code
  path was verified by a full MinGW-w64 cross-build (real Windows SDK
  headers) plus token-level proof of the macro-collision fix, and the
  workflow changes are enforced by the contract tests that run in every
  CI job.
