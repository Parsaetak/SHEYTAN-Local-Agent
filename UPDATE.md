# UPDATE.md — v1.2.1 SHEYTAN-LA CI Contract / Installer Hardening Package

**Release:** `v1.2.1` (codename Zeta) · **Base:** `main @ e260d53` (`v1.2.0`)
**Date:** 2026-09-15
**Package:** `SHEYTAN-Local-Agent-v1.2.1-UPDATE.zip`

This package is a targeted release-quality repair of the v1.2.0 tree: it
fixes the failing Linux ZIP verification (run `34871838054`) at the ROOT,
upgrades the Windows installer with a genuine desktop-shortcut option and a
clean upgrade/uninstall path, hardens the packaging gates, and expands the
bounded stress suite. It is a COMPLETE REPLACEMENT of the files it lists;
every path is relative to the repository root. No feature work and no
architecture changes were made.

The version moves `1.2.0` → `1.2.1` through the established identity chain
(package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE). The bump is deliberate: the app updater
(`internal/updater/appupdate.CompareVersions`) only offers an update for a
strictly higher version, so the improved installer must carry a new version
number.

What this package fixes, honestly and at the root:

1. **Linux ZIP verification failure (run 34871838054) — canonical
   package-root contract.** The Linux ZIP was staged and created under the
   `SHEYTAN-Local-Agent/` root while its verification step expected entries
   under `SHEYTAN-LA/` — and the release job repeated the same wrong
   literals. Deeper: `internal/releasecontract.RequiredLinuxZipEntries()`
   itself mixed roots (Windows-prefixed `README.txt` / `BUILD-INFO.txt` in
   the LINUX entry list), which is why the stress gate stayed green while
   CI failed: two copies of the truth agreeing with each other instead of
   with reality. The fix is one canonical variable per platform
   (`WIN_PKG_ROOT: "SHEYTAN-LA"`, `LINUX_PKG_ROOT: "SHEYTAN-Local-Agent"`
   in the workflow env block) from which every staging directory, ZIP name,
   ZIP entry check, artifact name and release-metadata reference derives —
   the verifier is not weakened, it is made structural. The Go contract now
   exposes the exact workflow-slot spellings
   (`WorkflowWinRootEnvLine`, `WorkflowLinuxRootEnvLine`,
   `WindowsAppDirWorkflowSlot`, `LinuxAppDirWorkflowSlot`,
   `RequiredWindowsZipWorkflowEntries`, `RequiredLinuxZipWorkflowEntries`)
   and the stress gate requires them in the workflow text. Any future
   reintroduction of a duplicated root literal fails the suite instead of
   the release.
2. **Stress gate strengthened in both directions.** `zeta_release_surface`
   now requires (a) the two canonical root env lines, (b) the staging-dir
   slot spellings, (c) every parameterized ZIP-entry spelling, and (d) the
   existing artifact-slot agreement — so the workflow cannot drop the
   canonical variables and the contract cannot drift from the workflow
   without failing the suite.
3. **Installer: desktop shortcut is now a genuine option.** The previously
   unconditional `CreateShortcut $DESKTOP\…` became a checkbox on the
   directory page (`Create a &desktop shortcut`, nsDialogs created in the
   page SHOW callback), DEFAULT CHECKED, state persisted across Back/Next;
   unchecked means no desktop shortcut; silent installs keep the checked
   default. The graphical directory page itself is unchanged and the
   selected `$INSTDIR` demonstrably drives every payload file, shortcut and
   registry entry.
4. **Installer: clean upgrade behaviour.** A running instance is closed
   before the payload is replaced: graceful `taskkill`, then a bounded
   retry (3 × 500 ms) on the locked executable, then an explicit
   Retry/Cancel message — no silent partial upgrade and no uncontrolled
   loop. The developer-path default `BUILDDIR` (script-relative) pointed
   one level short of the repository root
   (`packaging\nsis\..\dist` → corrected to `..\..\dist\windows\app\…`);
   the compile-time `!if ! /FileExists` assertion catches that class
   precisely instead of a generic "no files found".
5. **Installer: uninstaller hardening with user data preserved by
   construction.** `SetShellVarContext all` makes shortcut creation and
   removal symmetric (all-users Start Menu + common desktop). Uninstall
   removes the application, both shortcuts, the ARP registration and the
   AppUserModelID — and NOTHING else: plain `RMDir` can only remove an
   EMPTY install directory, so portable `models/` / `workspace/` trees
   inside `$INSTDIR` survive by construction, and
   `%LOCALAPPDATA%\SHEYTAN-LA` + the machine `SHEYTAN_DATA_DIR` variable
   are deliberately preserved (documented rationale kept). A new CI
   contract FORBIDS `RMDir /r` anywhere in the script and requires the
   `preserve user data` marker — the boundary is enforced, not remembered.
6. **NSIS source contract verified in CI before building.** The workflow
   greps `packaging/nsis/installer.nsi` for the required fragments
   (`MUI_PAGE_DIRECTORY`, the checkbox label, `BST_CHECKED`,
   `WriteUninstaller`, `UninstallString`, `SHEYTAN_DATA_DIR`,
   `preserve user data`) and rejects the build when any is missing or when
   a recursive delete appears — a weakened installer script can no longer
   compile its way into a release.
7. **Packaging verification hardening.** Both portable ZIP verifications
   now also read the in-package `BUILD-INFO.txt` and require the resolved
   `Version:` — the ZIP content is identity-checked, not just
   shape-checked. Every workflow job carries `timeout-minutes` (audit 40,
   Windows 60, Linux 50, release 20) so a hung step can never occupy a
   runner unbounded. The native engine build is cached in the audit and
   Linux jobs, keyed on the exact `hashFiles('native/engine/**')` source
   hash — a cache hit reuses the build tree for identical sources while
   ctest still runs; reproducibility is unaffected.
8. **Bounded stress suite expanded (31 → 45 scenarios).** New file
   `cmd/stress_release.go` (wired into `runStressSuite`): the application
   updater against offline / tampered-SHA-256 / size-mismatch / staged-
   drift / zip-slip-member hostility (loopback-only httptest servers, hard
   timeouts), the vision readiness state machine against missing overrides
   and text-only architectures, memory parallel-append uniqueness +
   idempotent missing-ID delete, session restart persistence + garbage-ID
   graceful failure, context-cache bounds and TTL expiry, context-plan
   budget allocation under a 10 MB transcript, recommendation extremes
   (degenerate hardware → notes, measured cores → threads, absurd
   configured context → clamped) and the LoopGuard retry/wall-clock
   ceiling (via the newly exported `agent.LoopGuard.WallClockExhausted`).
   The suite now ends with a machine-readable
   `STRESS-RESULT pass=N fail=M hangs=0 crashes=0` line.

## 1. Exact change map

```text
.github/workflows/build-desktop.yml   modified  canonical WIN_PKG_ROOT /
                                                LINUX_PKG_ROOT env; every
                                                packaging path derives from
                                                them; Linux ZIP verification
                                                fixed at the root; in-package
                                                BUILD-INFO version checks;
                                                NSIS source-contract step;
                                                job timeouts; native-engine
                                                cache (audit + linux)
cmd/stress.go                         modified  runStressSuite appends
                                                stressReleaseScenarios and
                                                prints STRESS-RESULT line
cmd/stress_zeta.go                    modified  gate 3e2 (canonical root env
                                                lines), 3h/3i (slots +
                                                parameterized entry
                                                spellings), staging check via
                                                workflow slots
cmd/stress_release.go                 NEW       14 bounded release-hardening
                                                scenarios (updater, vision,
                                                memory, sessions, context,
                                                recommendation, LoopGuard)
internal/agent/reliability.go         modified  exported
                                                LoopGuard.WallClockExhausted
                                                (thin accessor over the
                                                existing unexported check —
                                                no behaviour change)
internal/releasecontract/             modified  single-source contract for
  releasecontract.go                            package roots: env lines,
                                                slot spellings, staging
                                                slots, required-entry sets;
                                                RequiredLinuxZipEntries
                                                fixed to the Linux root
packaging/nsis/installer.nsi          modified  desktop-shortcut checkbox
                                                (default checked), clean
                                                upgrade (bounded retry +
                                                Retry/Cancel),
                                                SetShellVarContext all,
                                                uninstaller hardening with
                                                user data preserved by
                                                construction, corrected
                                                developer-path BUILDDIR
                                                default
package.json                          modified  version 1.2.0 → 1.2.1
package-lock.json                     modified  version 1.2.0 → 1.2.1
internal/config/config.go             modified  AppVersion "1.2.1"
                                                (release-version.mjs repair)
build/config.yml                      modified  productVersion "1.2.1"
                                                (release-version.mjs repair)
SIGNATURE                             modified  first line v1.2.1
                                                (release-version.mjs repair)
README.md                             modified  v1.2.1 section + release
                                                header
agent.md                              modified  v1.2.1 notes for the next
                                                agent
ARCHITECTURE.md                       modified  Part II.10 (release-contract
                                                machinery)
UPDATE.md                             modified  this document
worklog.md                            modified  v1.2.1 entry appended
```

## 2. DELETE list (files removed by this package)

None. No file is deleted.

## 3. DO NOT TOUCH list (this package deliberately leaves alone)

```text
internal/llm/**                 engine pairing, launch contract, vision
                                recording — untouched
internal/vision/**              state machine untouched (only consumed by
                                new stress scenarios)
internal/updater/updater.go     engine updater untouched
internal/api/**                 HTTP surface untouched
internal/native/engine/**       C++ engine + Go boundary untouched
src/** (frontend)               untouched; the committed web/static
                                rebuilds byte-identical
web/static/**                   regenerated during verification,
                                byte-identical to the committed bundle
internal/lab/**                 Coding Lab untouched
internal/platform/**            AUMID/firewall machinery untouched
scripts/release-version.mjs     identity chain untouched (still passes
                                --check against the new version)
```

## 4. Safe-apply procedure

1. Check out the base commit (`main @ e260d53`, v1.2.0) cleanly.
2. Replace every file listed in `REPLACEMENT-MANIFEST.txt` with the copy
   from this ZIP (all paths are repository-root relative).
3. Verify: `shasum -a 256 -c REPLACEMENT-SHA256.txt` (from the repository
   root, with the package's files in place) — every entry must read OK.
4. Run the verification battery (§6) — every command must pass.

## 5. Verification executed (automated, per release policy)

| Check | Result |
|---|---|
| `node scripts/release-version.mjs --check` | PASS (4/4 surfaces consistent at 1.2.1) |
| `npm ci` | PASS (0 vulnerabilities) |
| `npm run typecheck` | PASS |
| `npm run lint` | PASS (0 warnings, 0 errors, 28 files) |
| `npm run build` | PASS (`web/static` regenerated byte-identical) |
| `go build ./...` (GOOS=windows, CGO_ENABLED=0 — CI parity for the shipped binary) | PASS |
| `go test ./internal/... -tags headless -count=1` | PASS (36 packages) |
| `go test ./cmd/... -count=1` | PASS |
| `go vet ./...` (GOOS=windows, full tree — mirrors CI) | PASS (clean) |
| `cmake -S native/engine -B native/engine/build` + `cmake --build` | PASS |
| `ctest --test-dir native/engine/build --output-on-failure` | PASS (12/12) |
| Stress suite (`go build ./scripts/stress-main` + `stress`) | PASS — `STRESS-RESULT pass=45 fail=0 hangs=0 crashes=0` |
| `makensis` with CI-parity flags (`-DVERSION=1.2.1`, absolute BUILDDIR/OUTFILE) | PASS (installer compiled; uninstall section embedded) |
| Installer structural validation (MZ + PE header, UTF-16 version resources: ProductName `SHEYTAN-LA`, ProductVersion `1.2.1`, OriginalFilename identity, `NullsoftInst` signature) | PASS |
| NSIS source contract (directory page, checkbox default-checked, WriteUninstaller, UninstallString, SHEYTAN_DATA_DIR, preserve user data, no `RMDir /r`) | PASS |
| Workflow YAML parse check (4 jobs, env carries both canonical roots) | PASS |

Known environment exceptions (documented, not faked): the sandbox has no
GTK4/WebKitGTK development headers, so `internal/desktop` (the Wails shell)
cannot compile on this Linux host — `go test` covers the other 36 internal
packages and the full tree builds, vets and tests cleanly under
GOOS=windows CGO_ENABLED=0, which is exactly the shipped-binary CI path;
the pristine base fails identically. The Linux `makensis` port runs under
an LD_PRELOAD path shim (its compiled-in `/usr/share/nsis` data path is
redirected into a user prefix); on the Windows CI runner no shim is
involved. CI was not triggered from this environment (no push access).

## 6. What "verified" means here

- **CI contract fixed**: the Linux ZIP verifier, the release-job payload
  verifier, the Go contract and the stress gate all derive from ONE
  canonical root per platform; the gate fails on any future drift in
  either direction.
- **Stress suite passed**: 45/45 scenarios, machine-readable
  `STRESS-RESULT pass=45 fail=0 hangs=0 crashes=0`.
- **Installer validated**: compiled with the exact CI-parity invocation;
  PE identity resources carry ProductName `SHEYTAN-LA` and
  ProductVersion `1.2.1`; the source contract (directory selection,
  desktop-shortcut option default-checked, registered uninstaller,
  user-data preservation, no recursive deletes) is enforced before and
  after the build.
- **Uninstaller validated**: `WriteUninstaller "$INSTDIR\Uninstall.exe"` +
  ARP `UninstallString` + Start Menu entry + AUMID removal + plain-`RMDir`
  user-data boundary (recursive deletes contractually forbidden).
- **Desktop shortcut option validated**: checkbox on the directory page,
  `BST_CHECKED` default, state variable consumed by the install section,
  uninstall removes the shortcut only if it exists.
- **Package verified**: every artifact name, ZIP entry and in-package
  version is derived from the same canonical variables end-to-end.
- **Final ZIP created and byte-identical**: see §7.

## 7. Package contents and integrity

The ZIP contains exactly:

```text
UPDATE.md
REPLACEMENT-MANIFEST.txt
REPLACEMENT-SHA256.txt
.github/workflows/build-desktop.yml
agent.md
ARCHITECTURE.md
build/config.yml
cmd/stress.go
cmd/stress_release.go
cmd/stress_zeta.go
internal/agent/reliability.go
internal/config/config.go
internal/releasecontract/releasecontract.go
package-lock.json
package.json
packaging/nsis/installer.nsi
README.md
SIGNATURE
worklog.md
```

Excluded by design: `.git`, caches, `dist/` staging trees, native build
directories, model files, temporary files, and every unrelated generated
artifact. A byte-identical copy of the ZIP is placed at the repository root
and verified with SHA-256 before delivery.
