# UPDATE.md — v1.3.5 Windows CTest Completion / Native Test Hardening

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
