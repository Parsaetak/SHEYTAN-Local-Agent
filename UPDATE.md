# UPDATE.md — v1.3.6 Engine Lifecycle, System Discovery, Net Search & Canonical Data Root

**Release:** `v1.3.6` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 57f0c1b` (`1.3.5`) · **Date:** 2026-09-22
**Package:** `SHEYTAN-Local-Agent-v1.3.6-UPDATE.zip`
**ROADMAP.md:** untouched by this release (blob SHA verified unchanged:
`c7e2c1720eb5e97bd932c0d76100b8719193e650`).

This release root-fixes the engine ownership defect class reported in
the 2026-09-22 runtime evidence: engine startup raced the engine
updater, `0xC0000139` (STATUS_ENTRYPOINT_NOT_FOUND) was retried through
the compatibility ladder as if it were an argument problem, a process
answering `/health` on port 8080 could be adopted without any identity
proof, and the installer's hidden `SHEYTAN_DATA_DIR` environment
variable kept a second data root alive in AppData.

1. **One provisioning authority.** `internal/updater.InstallStaged` is
   now the only path that puts a llama.cpp engine into the managed bin
   directory: staged download → extraction → static candidate
   validation → atomic directory swap (old dir renamed aside, staging
   renamed in, non-engine files merged back) → installed-binary
   identity verification (SHA-256) → commit (tag + `engine-install.json`
   manifest) → cleanup. Any failure before the commit restores the
   previous engine directory; last-known-good is never destroyed and
   obsolete staging archives are removed. The in-place `copyAll`
   overwrite of a live bin directory is gone.

2. **Engine-owned transactional updates.** `LlamaServer.UpdateEngineNow`
   wraps the whole update in an EXCLUSIVE lifecycle operation: it holds
   `switchMu` start-to-finish (Start/Restart/prewarm can never
   interleave), stops the engine itself, installs through the single
   authority, restarts the exact installed binary and verifies
   readiness. `updater.UpdateEngineWithProgress` delegates to it
   (`TransactionalEngine`), so the scheduled updater and the manual
   updater both take the same engine-owned path.

3. **Identity-proven adoption (no blind port adoption).**
   `internal/proc.ListeningProcess` attributes a listening TCP port to
   a PID through real OS APIs (GetExtendedTcpTable +
   QueryFullProcessImageName on Windows; /proc/net/tcp +
   /proc/<pid>/exe on Linux — never cmd.exe/PowerShell/tasklist
   parsing). Adoption of a port-serving process now REQUIRES that its
   executable equals the managed engine path; a foreign process is
   refused with recorded evidence (PID + executable), never killed
   automatically.

4. **`0xC0000139` is a first-class diagnostic.** Loader-class Windows
   exit codes (0xC0000135/0xC0000139/0xC000007B/0xC0000142/… and Win32
   5/126/127/193) decode into actionable reports (class, summary,
   advice, binary identity, dependency evidence); when the OS exit code
   is truncated (Unix keeps 8 bits), the loader's own NTSTATUS text in
   the captured stderr is classified. A new ENGINE PREFLIGHT
   (`internal/llm/preflight.go`: exists → architecture → dependency
   closure via debug/pe → bounded `--version` probe, sharing the
   production subprocess runner) catches these failures BEFORE any
   model launch.

5. **No retry storm on deterministic failures.** Loader-class startup
   failures break the compatibility ladder immediately — the ladder
   remains only for genuine llama.cpp argument-layout problems (the
   bounded repair behavior is unchanged and still tested).

6. **System Engine Discovery.** `internal/engdiscovery` searches for an
   existing compatible llama.cpp / shtn-engine-host package before any
   download: Tier 0 (managed dir + persisted discovery cache),
   Tier 1 (PATH, exe dir, sibling bins, common user locations),
   Tier 2 (bounded parallel full-system scan — background only, never
   on the startup path). Validation is non-destructive: static checks
   first (format + architecture + PE import closure), a bounded
   `--version` probe last. Validated candidates are imported into the
   managed directory with byte-identical dedupe (SHA-256) and
   provenance recorded.

7. **Canonical data root + AppData migration.** The NSIS installer no
   longer writes the machine `SHEYTAN_DATA_DIR` env var (it DELETES it,
   retiring the 1.3.5 AppData contract), creates the install-local
   `<AppRoot>\data` tree and grants the built-in Users group modify
   rights (icacls, inheritable) so mutable data stays writable under a
   Program Files install. The runtime migrates a 1.3.5-era
   `%LOCALAPPDATA%\SHEYTAN-LA` root into the canonical root once —
   hash-verified, idempotent, restart-safe, engine bundle as a unit
   (two llama.cpp builds are never mixed), legacy root removed only
   after full verification, and an explicit user override is never
   touched.

8. **Net Search (Research tab removed, backend reused).** The Research
   workspace layer/tab/panel is removed with no dead route (an old
   `#research` hash resolves to Agent). The SAME research service now
   backs a per-request Net Search control in BOTH Chat and Agent
   composers (`Thinking · Tools · Net Search`), enforced server-side
   via `agent.WithNetSearch` (the research tool is authorized for that
   request only, recorded in telemetry) and reachable directly at
   `/api/net-search` (same handler as the `/api/research` shim — one
   implementation). Settings labels read "Net Search"; persisted JSON
   keys are unchanged.

9. **Visible version fixed.** The `appVersion ?? "v1.2.2"` fallback in
   `src/App.tsx` is replaced by a build-time canonical constant
   (`__APP_VERSION__` injected by Vite from package.json — the single
   release source); the backend's runtime version stays authoritative
   once loaded. A regression suite (`version-contract.test.ts`) blocks
   any stale version literal from returning to shipped source.

10. **Engine diagnostics surface.** `/api/engine` now carries a
    `diagnostics` block (binary path, recorded tag + probed version,
    decoded failure class, restart count, recent stdout/stderr, the
    full failure report and the last preflight evidence) so the UI can
    render the REAL engine state (Ready/Starting/Updating/Failed with
    the exact reason) instead of a generic spinner.

11. **Release identity.** Canonical `1.3.6` synced through
    package.json → `internal/config/config.go` (`AppVersion`),
    `build/config.yml` (`productVersion`), `SIGNATURE`. Version-only
    identity: tag `v1.3.6`, release title `1.3.6`, no codename, no
    suffix, no `APP_VERSION_FULL`.

## Verification status (truthful)

Verified locally on Linux x86-64 (Go 1.27.1, Node 24):

- `gofmt` clean (one pre-existing drift in `internal/research/service.go`
  also corrected); `go vet -tags headless ./internal/... ./cmd/...` clean.
- `go test ./internal/... -tags headless -count=1` — 51 packages ok,
  including the new suites: engine lifecycle/adoption/classification/
  transactional-update/race (`internal/llm`), installer transaction and
  rollback (`internal/updater`), process identity (`internal/proc`),
  static analysis (`internal/engcheck`), discovery
  (`internal/engdiscovery`), migration + path invariants
  (`internal/config`).
- Frontend: `npm ci`, `typecheck`, `lint` (0 warnings), `test:units`
  (96/96, including the new workspace/net-search/version contracts),
  `test:release` (28/28), `build`, `verify:web`; the stale `v1.2.2`
  literal is gone from the rebuilt `web/static` bundle.

NOT verified in this environment (remains CI / Windows-machine owned):

- Windows builds, Windows NSIS installer execution, Windows native C++
  CTest, Windows executable smoke tests, and the real-Windows runtime
  acceptance of §45.
- GitHub Actions runs (no commit pushed from this environment), the
  v1.3.6 tag, release publication, and published asset hashes.
- A real llama.cpp engine boot on user hardware.

---

# UPDATE.md — v1.3.5 Windows CTest Completion / Native Test Hardening (historical)


**Release:** `v1.3.5` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ a339b31` (`v1.3.4`) · **Date:** 2026-09-22
**Package:** `SHEYTAN-Local-Agent-v1.3.5-UPDATE.zip`
**ROADMAP.md:** untouched by this release (no roadmap feature is
shipped; the v1.3.4 Repository Intelligence slice is stabilized).

This release root-fixes the Windows CI completion path (Actions run
`35583009466`): the Windows runner configured CMake, built the
complete native engine, built all 12 test executables, and started
CTest — `10 PASS / 2 FAIL` (`tokenizer`, `generate`). Both failures
shared one root cause (POSIX-only temporary-path assumptions in the
native tests), and the deep audit that followed found and fixed two
further genuine portability defects plus one build-system defect.
No assertion was weakened; no test was skipped, disabled, or made
non-fatal.

1. **Root cause of the two Windows failures — POSIX temp-path
   assumptions.** `test_tokenizer.cpp` resolved its synthetic GGUF
   location with `getenv("TMPDIR")` falling back to the literal
   `/tmp`; `TMPDIR` does not exist on Windows, `/tmp` resolves to
   `<current-drive>:\tmp` (absent on the runner), the synthetic model
   never loaded, and the remaining 37 assertions cascaded.
   `test_generate.cpp` hard-coded `/tmp/shtn-not-llama.gguf`, so
   `fopen()` returned `nullptr` and the malformed-model block was
   skipped. Both tests now build their fixtures under a shared
   cross-platform helper (below).

2. **One reusable native-test helper — `tests/temp_dir.h` (new).**
   A single RAII `shtn_test::TempDir` for the whole native suite:
   resolves the platform temp root through
   `std::filesystem::temp_directory_path()` (GetTempPath on Windows;
   TMPDIR/TMP/TEMP then the platform default on POSIX — never a
   hard-coded path in callers), generates collision-free directories
   (process id + in-process counter + monotonic clock + random
   suffix), returns native paths that round-trip through stdio
   (spaces fully supported), and removes its whole tree on
   destruction. No `tmpnam`, no `TMPDIR`-must-exist assumption, no
   per-test `#ifdef` copies. The three previously-duplicated private
   copies (`test_gguf`, `test_model`, `test_host`) are consolidated
   onto it; `test_engine`'s `/definitely/not/here.gguf`
   nonexistent-path literal is now derived from the helper too.

3. **Cross-platform regression contracts (new test sections, same
   12 binaries).** `test_gguf` gains a portable temp-directory
   contract (create / write / read / close / remove, a path with
   spaces, uniqueness under repeated creation, RAII cleanup) and a
   mapped-file release contract (a mapped+closed file must be
   deletable). `test_model` gains an engine-level contract: after
   load + unload the model file must be deletable, and the engine
   must remain reusable.

4. **Windows engine defect fixed — `MappedFile` handle inversion
   (`native/engine/src/gguf.cpp`).** The Windows `open()` stored the
   file handle in `handle_` and the section handle in `map_`; the
   Windows `close()` then called `UnmapViewOfFile(section)` — which
   requires the VIEW BASE address — so the unmap silently failed,
   the section object leaked, and the still-mapped view pinned the
   `.gguf` file for the life of the process. On Windows this meant:
   every model unload leaked a section + view, and a model file
   could not be deleted or replaced after unload while the host
   process lived (a user-facing file-lock defect), and test temp
   cleanup could never remove a loaded model's file. `open()` now
   closes the file handle once the view exists (the section holds
   its own reference) and stores `handle_` = section, `map_` = view
   base, matching the class contract and the POSIX branch; `close()`
   unmaps the view and closes the section.

5. **`test_host` cwd-dependent fixture path fixed.** The Phase 5
   streaming tests resolved fixtures via
   `getenv("SHTN_FIXTURES_DIR")` with a `"../tests/fixtures"`
   fallback — a cwd-relative path that only worked under CTest
   (whose default working directory is the build dir). The test now
   uses the configure-time `SHTN_FIXTURES_DIR` macro (which CMake
   already defined for this target — it was dead configuration),
   exactly like `test_forward` and `test_generate`.

6. **`native/engine/Makefile` — `test_host` rule repaired.** The
   plain-make path never defined `SHTN_FIXTURES_DIR` (relying on the
   broken cwd fallback, so `make test` could never pass `test_host`
   from any working directory). The rule now bakes
   `-DSHTN_FIXTURES_DIR='"$(CURDIR)/tests/fixtures"'` like the
   forward/generate rules; `make -C native/engine test` passes 12/12.

7. **CI regression gate (new).** The audit job gains a "Native
   temp-path portability gate": `git grep` fails the build if any
   native source or test outside `tests/temp_dir.h` reintroduces a
   `"/tmp` literal or a direct `getenv("TMPDIR")` call.

8. **Release identity.** Canonical `1.3.5` synced through
   package.json → `internal/config/config.go` (`AppVersion`),
   `build/config.yml` (`productVersion`), `SIGNATURE`. Version-only
   identity: tag `v1.3.5`, release title `1.3.5`, no codename, no
   `-Z`, no suffix, no `APP_VERSION_FULL`. The codename gate returns
   zero matches.

## Verification (this release, local matrix)

- Native Linux: fresh `cmake --fresh` configure + build +
  `ctest --output-on-failure` → **12/12**; parallel `ctest -j4` →
  12/12; zero temp leftovers; `make -C native/engine test` → 12/12;
  every test binary also passes when executed from a foreign cwd.
- Native Windows: full cross-compile of the engine, host and all 12
  test binaries (clang 23 / llvm-mingw, x86_64-w64-windows-gnu,
  fully static) executed under Wine 11.18 (WoW64) → **12/12**,
  zero temp leftovers (the pre-fix v1.3.4 binaries reproduce both
  original failures plus the mapped-file lock in the same harness).
- Go: `gofmt` clean; `go vet -tags headless ./internal/... ./cmd/...`
  clean; race gate
  (`api, agent, sessions, contextplan, histref, runtime`) PASS;
  `go test ./internal/... -tags headless -count=1` → 47/47 packages
  ok, including `internal/repoindex` (v1.3.4 Repository Intelligence)
  and `internal/native/engine` (real-host Go↔C++ integration);
  stress suite 47/47.
- Frontend: `npm ci`, `typecheck`, `lint`, `test:units` (84/84),
  `test:release` (28/28), `build`, `verify:web --dist` all pass;
  `web/static` is byte-identical to the committed v1.3.4 build (no
  frontend source changed, so no regenerated assets ship).
- Release metadata: `release-version.mjs --check` consistent;
  codename gate zero matches; `APP_VERSION=1.3.5`.

No file was deleted by this release. The update ZIP contains only
the files listed in `REPLACEMENT-MANIFEST.txt`.
