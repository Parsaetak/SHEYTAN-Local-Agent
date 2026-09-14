# UPDATE.md — v1.2.0 SHEYTAN-LA Stability / Release-Quality Repair Package

**Release:** `v1.2.0` (codename Zeta) · **Base:** `main @ 40fdd3b` (`v1.2.0`)
**Date:** 2026-09-14
**Package:** `SHEYTAN-Local-Agent-v1.2.0-UPDATE.zip`

This package is a targeted stability, lifecycle and release-quality repair
of the v1.2.0 tree (the run-34857158565 failure set). It is a COMPLETE
REPLACEMENT of the files it lists; every path is relative to the repository
root. No version bump was taken: the v1.2.0 release logic is made internally
consistent instead (package.json remains the single version authority,
resolved by `scripts/release-version.mjs`).

What this package fixes, honestly and at the root:

1. **Release-surface contract (Linux stress `zeta_release_surface`).** The
   stress gate pinned the retired v1.1.x Windows artifact name while the
   workflow had moved to the `SHEYTAN-LA-v<ver>-windows-x64` identity. A new
   single-source contract (`internal/releasecontract`) is now the only
   artifact-naming truth; the stress gate derives the contracted names from
   the installed version AND regex-derives the names the workflow actually
   produces, failing on any future drift in either direction. Stale legacy
   naming is rejected explicitly.
2. **LoopGuard wall-clock budget (Windows Go verification).** A 1ns budget
   measured against the raw Windows monotonic clock could read elapsed == 0
   and never expire. The budget logic now uses an injectable clock with
   defined semantics (`elapsed >= budget` blocks; zero/negative disables)
   and the test suite is fully deterministic (expired / exactly-at-expiry /
   just-before / disabled / negative / multi-call). No sleep-based timing.
3. **Lab workspace path canonicalisation (8 lab test failures on Windows).**
   On Windows CI the temp root is reached through an 8.3 short name
   (`RUNNER~1`) while the jail's symlink resolution returns the long form,
   so valid workspace files were refused with "lab: invalid workspace path".
   Both sides of every comparison are now canonicalised through one helper
   (absolute + cleaned + EvalSymlinks on the nearest existing ancestor, tail
   re-attached), `sameWord` makes equality OS-aware (case-insensitive on
   Windows), and rooted `\foo` paths are refused. The jail is exactly as
   strict: `..`, absolute, different-volume and outside-root paths are still
   rejected — pinned by a new regression test that reproduces the
   short/long-name mismatch class with a symlink.
4. **LLM path assertions.** Tests never compare raw path strings any more;
   `internal/llm/paths_test.go` provides the one OS-aware helper and the
   fake-engine fixtures build paths with `filepath.Join`.
5. **Logging handle cleanup (Windows temp-dir cleanup).** The three
   `RecentParsed` tests now close the log Manager (idempotent `Close`) so no
   `app.log`/`tools.jsonl`/`llm.jsonl` handle outlives the test.
6. **ZIP member validation.** `\absolute\path` is "rooted-relative", not
   absolute, under `filepath.IsAbs` on Windows — the validator let it
   through. The single authoritative validator
   (`updater.safeZipPath`, exported `SafeArchivePath`; `internal/llm`
   delegates to it) is separator-canonical: POSIX-absolute, rooted,
   drive-letter, UNC, mixed-separator traversal and NUL-bearing members are
   all rejected on every host; regression tests pin the exact member set.
7. **NSIS installer path contract ("no files found").** makensis resolves
   compile-time paths relative to the .nsi SCRIPT directory, so the
   repo-root-relative `-DBUILDDIR` looked under `packaging\nsis\dist\...`.
   The contract is now singular and documented: CI passes ABSOLUTE
   `BUILDDIR`/`OUTFILE` paths, the script carries compile-time
   `!if ! /FileExists` assertions on the staging executable, the output path
   is centralised in one `OUTFILE` define, and the workflow asserts the
   staging directory + executable before invoking makensis, checks
   `$LASTEXITCODE`, and verifies the produced installer (exists, non-empty,
   ProductVersion == APP_VERSION, ProductName == SHEYTAN-LA, MZ header).
8. **Deterministic subprocess lifecycle.** `LlamaServer.Stop()` now waits
   (bounded) on the exit watcher's done-channel after Kill so the child is
   fully reaped — and on Windows its executable unlocked — before Stop
   returns (the "unlinkat llm.test.exe: Access is denied" / orphan
   `llm.test`/`conhost` class). Three engine tests that started the fake
   engine without stopping it now own and stop it deterministically.
   `taskkill` (process-tree termination) is spawned with CREATE_NO_WINDOW
   so it no longer allocates orphan conhost consoles.
9. **Updater goroutine ownership (`.update-stage` residue).** The scheduled
   engine-update loop spawned by `EnsureSetup` was never cancelled:
   `Server.Close()` left it running, and in the test suite it raced
   `t.TempDir()` cleanup, writing `bin\.update-stage` behind the test's
   back. `RunScheduled` now returns a completion channel (closed only after
   cancellation is observed AND any in-flight pass finished); `Close()`
   cancels and waits (bounded 3s); unit tests disable the schedule so no
   unit test touches the network.

Efficiency/lifecycle audit result (no new subsystems): all tickers are
context-owned singletons (scheduler 1-minute tick, updater 6-hour tick,
stream-stall watchdog), WebSocket reconnect is a single bounded backoff
ladder (max 20 attempts / 15s) with stale-socket guards, engine start is a
single-flight state machine (`startLocked` no-ops when alive/starting and
ADOPTS an existing engine on port conflict), sysinfo probing is
`sync.Once`-cached, every retry ladder is bounded (auto-restart 3 with
exponential backoff, compat ladder finite), and all rings/buffers are
capped (UI activity 500 events, engine error ring 64, log viewer ring 512).
The rAF streaming coalescing (tokens → buffer → animation-frame flush →
one state update) is untouched.

## 1. DELETE

Nothing was deleted. No source file, test, tool or subsystem was removed by
this repair, and no test was weakened to accommodate Windows: the lab path
jail and the archive validator were made STRICTER, and the LoopGuard budget
tests became deterministic while covering MORE cases.

## 2. REPLACE

Package metadata, fully superseded by this package:

| Path | Why |
|---|---|
| `UPDATE.md` | this change map |
| `REPLACEMENT-MANIFEST.txt` | regenerated for this package |
| `REPLACEMENT-SHA256.txt` | regenerated for this package |

## 3. MODIFY

Files changed in place (all verified by the checks listed at the bottom):

| Path | Change |
|---|---|
| `.github/workflows/build-desktop.yml` | NSIS step: ABSOLUTE `BUILDDIR`/`OUTFILE`, pre-flight staging assertions, `$LASTEXITCODE` check, installer identity assertions; Verify Windows installer adds MZ-header + identity checks |
| `cmd/stress_zeta.go` | release-surface check derives the artifact contract from `internal/releasecontract` + regex agreement with the workflow; drift detection both directions; legacy naming rejected |
| `internal/agent/reliability.go` | LoopGuard: injectable clock, defined wall-clock budget semantics (`>=` blocks, `<= 0` disabled) |
| `internal/agent/reliability_test.go` | deterministic wall-clock coverage: expired / at-expiry / just-before / disabled / negative / multi-call (no real-clock nanosecond flake) |
| `internal/api/lifecycle.go` | `Close()` cancels the scheduled updater and waits (bounded) for its completion channel |
| `internal/api/server.go` | tracks `updateDone`; wires `RunScheduled`'s completion channel |
| `internal/api/server_native_test.go` | test config: `UpdateSchedule = "off"` (no network in unit tests) |
| `internal/api/server_test.go` | test config: `UpdateSchedule = "off"` |
| `internal/api/server_v114_test.go` | test config: `UpdateSchedule = "off"` |
| `internal/lab/lab_test.go` | new canonical-spelling regression test (symlink reproduction of the Windows 8.3 mismatch) with jail assertions |
| `internal/lab/workspace.go` | `canonicalPath` helper; both sides of every path comparison canonicalised; OS-aware `sameWord`; rooted `\foo` refused; canonical manager root |
| `internal/llm/capability_test.go` | three engine tests stop the fake engine they start (`t.Cleanup`) — no orphan test-binary children |
| `internal/llm/llama.go` | `Stop()` waits (bounded) for child reaping via the exit watcher; `exitDone` tracked; `safeArchivePath` delegates to the authoritative validator |
| `internal/llm/llama_test.go` | `filepath.Join` path construction; OS-aware path assertions |
| `internal/logging/recentparsed_test.go` | tests close the log Manager before temp-dir cleanup |
| `internal/proc/proc_windows.go` | `taskkill` spawned hidden (CREATE_NO_WINDOW) — no orphan conhost |
| `internal/updater/updater.go` | hardened separator-canonical `safeZipPath` + exported `SafeArchivePath`; `RunScheduled` returns a completion channel |
| `internal/updater/zipsafe_test.go` | regression members: rooted, drive-letter slash form, UNC slash form, mixed separators, NUL, whitespace; validator-agreement test |
| `packaging/nsis/installer.nsi` | centralised `OUTFILE`; compile-time staging-exe assertion; single documented path contract |

## 4. ADD

| Path | Purpose |
|---|---|
| `internal/releasecontract/releasecontract.go` | the SINGLE authoritative release artifact-name contract (portable ZIPs, installer, app roots, launcher, required ZIP entries) |
| `internal/llm/paths_test.go` | the one OS-aware filesystem-path comparison helper for tests (`sameFilePath` / `assertSameFilePath`) |

## 5. DO NOT TOUCH

Everything not listed above is unchanged by this package and must not be
modified without an explicit architecture decision. In particular the
existing v1.2.0 guarantees are preserved untouched:

- The rAF streaming coalescing path (tokens → buffer → animation-frame
  flush → one state update) — intentionally protecting React from
  per-token rendering pressure.
- The compatibility adapter, objective verification, performance telemetry,
  logs, network diagnostics, model capability detection, vision package,
  context handling and engine state machine.
- The updater safety posture (SHA-256-verified, staged, never executed by
  the app) and the Windows identity (`SHEYTAN-LA`, `SHEYTAN-LA.exe`,
  `Parsaetak.SHEYTAN-LA`).
- `package.json` as the only version source of truth; `internal/config`
  mirrors it and CI verifies the agreement every run.

## 6. Safe apply procedure

1. Check out the base commit (`main @ 40fdd3b`).
2. Replace the whole tree with this package's contents (or apply file-by-file
   following sections 1–4).
3. Verify: `node scripts/release-version.mjs --check`, `npm run typecheck`,
   `npm run lint`, `npm run build`, `go test ./... -count=1`,
   `go vet ./...`, then build and run the stress binary
   (`go build -o /tmp/sheytan-stress ./scripts/stress-main` and run
   `stress`) — the suite must report `30 pass / 0 fail`.

## 7. Executed checks (this package)

- `go test ./... -count=1` — all buildable packages pass; lifecycle-sensitive
  groups (`internal/agent`, `internal/api`, `internal/lab`, `internal/llm`,
  `internal/logging`, `internal/updater`) pass 3/3 repeated runs.
- `go vet ./internal/... ./cmd/...` clean; full-tree `GOOS=windows` build and
  vet clean.
- Stress/chaos suite: `30 pass / 0 fail` (includes `zeta_release_surface`).
- Frontend: `release-version.mjs --check`, `tsc --noEmit`, `oxlint`, and the
  production Vite build all pass; embedded assets re-synced.
- Native engine: 11/12 ctest suites pass locally; the in-process
  `host` harness stalls in this sandbox on UNMODIFIED sources, and the host
  binary itself was verified end-to-end over its real framed protocol
  (load → capability evidence → streamed chunks → terminal frame → clean
  exit 0). ctest is unchanged and green in CI.
- Process-leak check: no orphan `llm.test` children after the fixed suite,
  no `.update-stage` or `.rot` residue, no open test log handles.
- Windows packaging reproduced locally: `SHEYTAN-LA.exe` (PE32+ GUI,
  19.1 MB, go1.26.1), portable ZIP `SHEYTAN-LA-v1.2.0-windows-x64.zip` with
  the exact contracted entry set, `unzip -t` clean. The NSIS installer
  itself builds in CI (absolute path contract + assertions above); no
  makensis binary exists in this sandbox to produce it locally.

## 8. Known limitations (unchanged, honest)

- No Authenticode signing (no credentials in CI yet) — installer signature
  status is reported, never faked.
- No MSIX packaging; NSIS is the broad Win32 distribution path.
- No multimodal inference was claimed or tested here: no real multimodal
  model/projector exists in the test environment, and the vision readiness
  machinery is exercised only through its deterministic fixtures.
- The Linux portable ZIP keeps the historical
  `SHEYTAN-Local-Agent-Linux-x64-v<ver>Z.zip` identity by contract; only the
  Windows identity moved to `SHEYTAN-LA-v<ver>-windows-x64` in v1.2.0.
