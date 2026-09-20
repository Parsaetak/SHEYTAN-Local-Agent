# UPDATE.md — v1.3.1 Product Polish, Stable File Structure & Repository Cleanup

**Release:** `v1.3.1` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 8676582` (`v1.3.0`) · **Date:** 2026-09-20
**Package:** `SHEYTAN-Local-Agent-v1.3.1-UPDATE.zip`
**ROADMAP.md:** byte-identical to the locked baseline (git blob SHA-1
`c7e2c1720eb5e97bd932c0d76100b8719193e650`) — verified before and after
all work.

This release professionalizes the v1.3.0 tree without touching working
architecture: the product identity becomes **version-only**, the embedded
frontend moves to **stable deterministic filenames**, the generated tree
becomes exactly reproducible, the release/version workflow is simplified
to one canonical source, CI gains explicit hygiene gates, and the
historical version-suffixed test pile is consolidated into
behavior-oriented files. Every fix targets repository quality, not
features — all existing functionality is preserved.

---

## S1. Version-only product identity (IMPLEMENTED, TESTED)

- The retired product codename is removed from EVERY surface: runtime
  constants (`internal/config/config.go` no longer declares a codename
  constant), CI (`build-desktop.yml` carries no codename outputs, env
  vars, package text or assertions), release scripts
  (`scripts/release-version.mjs` no longer parses, defaults, emits or
  verifies codenames), installer/portable package text
  (`README.txt` / `BUILD-INFO.txt` carry `SHEYTAN-LA` + `Version:` +
  `Platform:` + `Commit:` only), README/ARCHITECTURE/UPDATE/agent.md/
  worklog, research User-Agent strings, stress-suite names, test names
  and comments.
- `scripts/release-version.mjs` is the single release-metadata gate:
  it validates **plain semver** (`MAJOR.MINOR.PATCH`), reads one
  canonical version from `package.json`, synchronizes the derived
  surfaces (`config.go` `AppVersion`, `build/config.yml`
  `productVersion`, `SIGNATURE` first line) and verifies the CI
  derivation shape. `--env` emits exactly `APP_VERSION` and
  `APP_VERSION_FULL` (a compatibility alias equal to `APP_VERSION` —
  no third variable, no suffix, no codename).
- Research providers (GitHub, Reddit, DuckDuckGo, SearXNG) identify as
  `SHEYTAN-Local-Agent/<AppVersion>` — derived from the same runtime
  constants the whole application reports (`research.DefaultUserAgent`),
  with the user-configurable `Config.EffectiveResearchUserAgent()` still
  honored by the Reddit provider. No provider hardcodes a release
  identity.
- The Linux portable ZIP and the release tag drop the historical `Z`
  suffix: `SHEYTAN-Local-Agent-Linux-x64-v1.3.1.zip` and tag `v1.3.1`
  (`internal/releasecontract` updated in lockstep with the workflow;
  the stress gate enforces the agreement).

## S2. Stable frontend filenames (IMPLEMENTED, TESTED)

- `vite.config.ts` emits deterministic names at the BUILD level (not
  post-build renames): `entryFileNames/chunkFileNames` =
  `assets/[name].js`, `assetFileNames` = `assets/[name][extname]`.
  Code splitting and lazy loading are fully preserved — every lazy
  chunk (AgentBody, SettingsPanel, SystemPanel, WorkspacePanel, LabPanel,
  ResearchPanel, AgentHeader, AgentSidebar, DownloadProgress,
  useResource, vision) is itself stable.
- The embedded tree now contains exactly:
  `web/static/index.html`, `web/static/assets/index.js`,
  `index.css`, `rolldown-runtime.js` and the stable component chunks —
  the same physical file keeps the same name across builds, so the
  agent can always identify what changed.
- No content-hash filenames, no duplicate component bundles, no
  orphaned CSS, no `.vite/manifest.json`.

## S3. Authoritative generated tree (IMPLEMENTED)

- The production flow is `clean dist → npm run build → stable assets →
  sync-web → web/static`; the workflow performs an explicit `rm -rf
  dist` (Windows: `Remove-Item dist`) before every production build and
  `sync-web.mjs` replaces `web/static` wholesale, so no
  previous-generation asset can survive.
- All old hashed bundles (`index-DRb7l71I.js`,
  `AgentBody-jPzETmLT.js`, `SettingsPanel-CvoMBB6r.js`, …) are deleted
  from the repository; `web/static` now exactly reflects the current
  clean build.
- `scripts/verify-static-assets.mjs` (new, wired into package.json as
  `npm run verify:web`) enforces the acceptance contract: required
  entries exist, every index.html reference resolves, every
  dynamic-import chunk exists, no hashed filename patterns, no vite
  manifest, and (with `--dist`) `web/static` mirrors the fresh build
  file-for-file.

## S4. CI hardening (IMPLEMENTED)

All existing quality gates are unchanged (Windows/Linux builds, native
C++ build + ctest, frontend typecheck/lint/unit tests, race gate, Go
tests + vet, stress suite, packaging, NSIS installer + source contract,
release manifest, SHA256SUMS, published-release verification). Added:

- **Codename removal gate** — a case-insensitive repository scan for the
  retired terms (the removed constant names, the old user-agent identity
  and the standalone codename word) fails the build on ANY match
  (tracked files only; ROADMAP.md is verified clean).
- **Stable frontend asset contract** — `verify-static-assets.mjs --dist`
  runs in the audit, Windows and Linux jobs.
- **Stale generated files** — the `--dist` mirror comparison fails CI if
  `web/static` contains anything the current build does not produce.
- **Version consistency** — the identity derivation
  (`package.json → release-version.mjs --env → APP_VERSION`) is
  shape-checked by the release script itself; the workflow verifies the
  derived surfaces against the resolved version in every job.

## S5. Test-file consolidation (IMPLEMENTED, ALL TESTS PASS)

- 24 version-suffixed files (`*_v114/v125/v126/v127/v1281/v129/v130_
  test.go`) and 5 phase-suffixed files were consolidated into canonical
  behavior-oriented test files — merged into existing canonicals where
  they exist (`handoff_test.go`, `preflight_test.go`,
  `runtransport_test.go`, `run_crossmode_test.go`,
  `run_settlement_test.go`, `server_test.go`, `contextplan_test.go`,
  `histref_test.go`, `scheduler_test.go`, `sessions_test.go`,
  `pipeline_test.go`, `attachments_test.go`, `contextcache_test.go`,
  `memory_test.go`, `recall_test.go`) and renamed where they are the
  package's only tests (`authority_test.go`, `measurement_test.go`,
  `config_patch_test.go`, `artifacts_test.go`, `policy_test.go`,
  `client_test.go`, `blankwarn_test.go`, `mcp_test.go`,
  `probelogging_test.go`).
- Coverage is preserved or strengthened: every test function moved
  verbatim; measurement tests renamed to behavior names
  (`TestFirstPromptCostMeasurement`, `TestThinkingSimplePromptStaysCompact`,
  `TestTrivialChatOffersNoToolsAndCompactBriefing`).
- The stress suite keeps full scenario coverage; the codename-branded
  file is renamed `cmd/stress_release_surface.go` with neutral function
  and scenario names (`release_surface`, `memory_unique_ids`,
  `trimlogs_rotate`).

## S6. Documentation professionalization (IMPLEMENTED)

- README: current product/architecture/installation/usage/capabilities
  first; a v1.3.1 release section; the accumulated per-release tables
  collapsed into a one-line-per-release history summary (full evidence
  remains in `worklog.md`).
- ARCHITECTURE: current subsystem ownership; historical release-label
  narratives rewritten as current-state contracts.
- agent.md: rewritten as a current-state-first engineering handoff (see
  the file itself).
- worklog: engineering evidence preserved; codename terminology
  eliminated; historical `v1.x.yZ` labels rewritten to plain versions.
- Historical `v1.x.yZ` labels across the entire codebase (95 files)
  were rewritten to plain version identifiers — version/date/change
  information preserved, only the removed codename suffix dropped.

## S7. Verification performed (this release)

- `gofmt` clean across `./cmd ./internal ./scripts`.
- `go build -tags headless ./...` OK; `go vet -tags headless ./...` clean.
- `go test -tags headless -count=1 ./internal/...` — all packages pass
  (0 FAIL).
- `go test -race -tags headless -count=1` on the concurrency-heavy
  packages (api, agent, sessions, contextplan, histref, runtime) — pass.
- Frontend: `npm ci`, `npm run typecheck`, `npm run lint`,
  `npm run test:units`, clean `npm run build` — all pass;
  `verify-static-assets.mjs --dist` satisfied.
- Native C++ engine: clean CMake configure + build + ctest (see final
  report for the exact matrix on this host).
- Stress suite: full run via `scripts/stress-main` (see final report).
- `node scripts/release-version.mjs --check` consistent (canonical
  `1.3.1`).
- Repository-wide scans: zero codename matches; zero content-hash asset
  filenames; zero stale generated files; ROADMAP.md blob SHA-1
  unchanged before and after.
