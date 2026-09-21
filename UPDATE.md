# UPDATE.md — v1.3.2 Release Repair + Native Engine Execution Hardening

**Release:** `v1.3.2` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 95b86ca` (`v1.3.1`) · **Date:** 2026-09-21
**Package:** `SHEYTAN-Local-Agent-v1.3.2-UPDATE.zip`
**ROADMAP.md:** byte-identical to the locked baseline (git blob SHA-1
`c7e2c1720eb5e97bd932c0d76100b8719193e650`) — verified before and after
all work.

This release root-fixes the Windows CI failure of run `35542000811`,
collapses release-metadata verification into ONE canonical cross-platform
validator, removes the redundant `APP_VERSION_FULL` identity alias, and
hardens native engine execution as a first-class release objective:
observable engine selection (no silent fallback), a pinned native
execution test contract (deterministic startup, model-path failures,
shutdown-during-generation, orphan-process prevention), real native
execution coverage in the Windows pipeline, and a contract-driven rewrite
of the stable-asset verifier. No working architecture was changed.

---

## S1. Root fix: one canonical release-metadata gate (IMPLEMENTED, TESTED)

- **Root cause of CI run 35542000811** (reproduced and verified): the
  Windows job's "Verify release metadata" step built the pattern
  `AppVersion\s*=\s*"<version>"` and passed it to PowerShell's
  `Select-String -SimpleMatch`, which treats regex escapes as LITERAL
  text — the pattern could never match `config.go`'s real content
  (`AppVersion = "1.3.1"`), so the Windows job failed at that step on
  every run while Linux passed (its grep semantics matched the regex).
- `scripts/release-version.mjs` is now the single canonical
  release-metadata authority. Its logic is exported as pure functions and
  the CLI operates from the working directory; modes: sync (default),
  `--check` (verify only, GitHub-Actions annotations), `--env` (emits
  exactly `APP_VERSION=<version>` — machine-readable only).
- The three per-shell reimplementations are gone: the audit job's bash
  greps, the Windows `Select-String` block, and the Linux job's bash
  greps all became `node scripts/release-version.mjs --check`. Shell
  steps now only orchestrate the validator.
- The validator's own **workflow contract** enforces the architecture in
  both directions: every build job (audit, windows, linux) must invoke
  the canonical check, and banned fragments fail the gate if a per-shell
  reimplementation ever reappears — including the exact
  regex-escapes-in-PowerShell pattern class that broke run 35542000811
  and the four bash grep reimplementation spellings.
- `APP_VERSION_FULL` is REMOVED throughout (audit outputs, job envs,
  release job env, `--env` emission, comments). The v1.3.2 audit found
  no consumer that distinguished it from `APP_VERSION` — it was a pure
  alias and a second identity variable with zero semantic value.
- Semver validation is stricter: leading-zero components (`1.03.2`) are
  rejected, exactly `MAJOR.MINOR.PATCH` passes.

## S2. Release-metadata regression suite (IMPLEMENTED, ALL TESTS PASS)

- New `scripts/release-version.test.mjs` (21 tests, wired as
  `npm run test:release`, run by every CI build job): valid/invalid
  semver shapes (suffixes, wildcards, whitespace, leading zeros),
  drift detection on every derived surface, missing-marker reporting,
  whitespace/gofmt-alignment variation, CRLF line endings (Windows
  checkouts) including byte-preserving repair, `--env` identity
  emission (exactly one variable; the retired alias must stay dead),
  workflow-contract violations (missing `--env` derivation, too few
  `--check` invocations, the 35542000811 pattern class, bash grep
  reimpls), and patch-to-patch version changes (1.3.1 → 1.3.2 repair
  across all surfaces, downgrade drift detection, alignment-preserving
  splice).

## S3. Observable native engine selection — no silent fallback (IMPLEMENTED, TESTED)

- New `llm.GenerationFallbackReporter` interface +
  `SelectGenerationBackendDetailed` returning a `BackendDecision`
  (serving backend + the reason a native selection fell back).
  `SelectGenerationBackend` remains the routing shorthand over the same
  single policy.
- The native `engine.Backend` implements
  `GenerationFallbackReason()`, distinguishing the three infrastructure
  classes instead of a generic "not capable": engine not running (with
  state + detail), running without a model loaded, and model loaded but
  not natively executable (the engine's own verdict).
- `runtime.Stack.streamGeneration` records and logs the reason whenever
  the user's explicit native selection is routed to llama.cpp
  (`recordNativeFallback` / `Stack.NativeFallback()`), and `/api/engine`
  exposes `fallbackReason` + `fallbackCount` in the native status block
  for the UI. The llama.cpp fallback remains the documented behavior —
  it is simply never a SILENT one anymore.
- Stale Phase-1-era comments corrected in `internal/llm/backend.go`,
  `internal/api/engine.go`, and the integration tests (generation has
  been REAL since Phase 5); `TestBackendGenerationNotImplemented`
  renamed to `TestBackendGenerationLifecycleGuard` to match its actual
  Phase-5 semantics.

## S4. Native execution test contract (IMPLEMENTED, ALL TESTS PASS)

New `internal/native/engine/execution_contract_test.go` pins the
execution properties beyond compilation, complementing the existing
Phase 4/5/7 real-host suites (real inference, streaming, cancellation,
lane recycling, context overflow — all retained):

- executable discovery: `DefaultHostPath` override + platform-suffixed
  default (`{dataDir}/bin/shtn-engine-host[.exe]`), `Available()`
  tracking the resolved path exactly;
- deterministic startup: protocol-mismatch handshake rejection fails
  closed with the mismatch diagnostic (no surviving process) and the
  engine stays retryable;
- bounded boot: a host that never answers the handshake is torn down
  deterministically by the caller's context (new fake-host `hang`
  mode) — no hang, no orphan;
- invalid/missing model path (real C++ host): clean rejection, model
  concern walks to `failed`, the ENGINE stays healthy, and a valid load
  recovers — infrastructure failures never convert to engine failures;
- shutdown during active generation (real host): Stop aborts the
  in-flight generation without wedging, the host process is fully
  terminated (signal-0 probe on Unix), and the engine restarts cleanly;
- orphan prevention: repeated start/stop cycles leave no host process
  behind (pid cleared + signal-0 probe after every stop);
- fallback-reason observability across engine states.

## S5. Windows pipeline reaches native execution (IMPLEMENTED)

- The Windows job now builds and ctests the native C++ engine BEFORE the
  Go tests (clean `--fresh` configure, Release build, `ctest -C
  Release`, multi-config staging of `shtn-engine-host.exe` at the path
  the Go tests resolve) — so the `TestRealCppHost*` integration suites
  execute against the REAL engine on Windows instead of skipping. The
  Go↔C++ boundary is subprocess IPC (no cgo), so this needs no toolchain
  change.
- With the metadata gate fixed (S1), the Windows pipeline now reaches
  its complete downstream stages: native engine build + ctest, real
  native execution in the Go suites, executable build + smoke probe,
  portable ZIP, NSIS installer, and both verification steps.

## S6. Contract-driven stable-asset verifier (IMPLEMENTED, TESTED)

- `scripts/verify-static-assets.mjs` now DERIVES the contract instead of
  guessing at name shapes:
  - **build-config contract**: `vite.config.ts` must declare exactly
    `assets/[name].js` (entries + chunks) and `assets/[name][extname]`
    (assets), with no hash tokens — a content hash can only enter a
    filename through these patterns, so asserting them asserts the whole
    no-hash property;
  - **reachability**: every file under `web/static` must be reachable
    from `index.html` through real reference edges (html src/href, JS
    import specifiers including dynamic imports and the
    `__vite__mapDeps` dependency arrays, CSS `url(...)`). A planted
    stale/hashed file is unreachable by construction and fails.
- The old heuristic (rejecting any name with a hyphen + 6+ chars
  containing a digit) is GONE: it could false-reject legitimate
  deterministic names carrying version digits. Verified by a dedicated
  negative/positive case suite: planted hashed file rejected, referenced
  version-digit name accepted, unreferenced foreign file rejected,
  missing referenced asset rejected, vite.config hash regression
  rejected, disabled code splitting rejected, pristine tree passes.
- The dynamic-import requirement is retained WITH its justification:
  the application is intentionally code-split (the entry lazy-loads the
  UI panels; per-panel stable filenames are part of the contract), so
  zero dynamic imports means splitting was silently disabled.
- `web/static` exactly mirrors a clean `dist` (unchanged `--dist`
  mirror rule); deterministic filenames and the dist → static
  synchronization contract are preserved.

## S7. Verification performed (this release)

Evidence-backed results from the v1.3.2 working tree (see the release
report for the full list): frontend `npm ci` + `typecheck` + `lint` +
`test:units` + clean `build` + `verify:web --dist` + the new
`test:release` suite; Go suites across `./internal/...` (headless) and
`./...`, race detector on the concurrency-heavy packages, `go vet`;
native engine clean `--fresh` configure, full build, 12/12 ctest, and
the complete Go↔C++ real-host suite (including the new execution
contract); the stress suite including the release-surface gate;
`release-version.mjs` sync + `--check` + `--env` consistent at canonical
1.3.2; ROADMAP.md verified byte-identical before and after.

Windows and Linux release PACKAGES are produced by the GitHub Actions
pipeline (the Windows runner is required for the Windows build); the
workflow changes in this release are validated structurally (YAML
validity, canonical check invocations, stress-gate contract fragments)
and by the release-metadata regression suite.
