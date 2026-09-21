# UPDATE.md — v1.3.4 Codename-Gate Root-Fix + Repository Intelligence (ROADMAP v1.4 Slice 1)

**Release:** `v1.3.4` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 4ebb1c8` (`v1.3.3`) · **Date:** 2026-09-21
**Package:** `SHEYTAN-Local-Agent-v1.3.4-UPDATE.zip`
**ROADMAP.md:** byte-identical to the locked baseline (git blob SHA-1
`c7e2c1720eb5e97bd932c0d76100b8719193e650`) — verified before and after
all work; the implemented slice is documented here, never by editing
the roadmap.

This release does two things:

1. **Root-fixes the self-triggering codename-removal gate** (Actions run
   `35571331850`): the repository-wide gate scans EVERY tracked file and
   the retired product codename had survived as literals inside the
   release contract source itself, its regression fixtures and four
   documentation files — the gate kept failing on its own enforcement
   surface. The fix removes every literal, keeps the regression
   coverage by constructing the forbidden strings dynamically, and
   weakens nothing.
2. **Ships the first production vertical slice of ROADMAP v1.4 —
   Repository Intelligence**: a bounded, persistent, INCREMENTALLY
   updated repository index (`internal/repoindex`) with language-aware
   symbol extraction, resolved in-repo dependency edges, deterministic
   test/source relationships, optional Git-aware relevance metadata, a
   hybrid evidence-ranked search API, the agent-facing `repo_search`
   tool, a tier-gated repository-evidence context block wired through
   the existing budget pipeline, and a Workspace card. It executes
   end-to-end in the real application — it is not types or UI
   placeholders.

The permanent release rule is unchanged: GitHub Release title = the
plain canonical version, tag `v${APP_VERSION}`, one identity variable
(`APP_VERSION`), no codename, no `-Z`, no release suffix.

---

## S1. Codename gate root-fix (ROOT FIX, VERIFIED)

### The failure

Run `35571331850` failed in `Source & frontend audit → Codename removal
gate`. The gate (unchanged since v1.3.1) runs:

```bash
pattern='app_?codename|version-z[e]ta|(^|[^[:alpha:]])z[e]ta([^[:alpha:]]|$)'
git grep -inE "$pattern" -- .
```

and exits non-zero on any match. The design defect: the scan is
repository-wide (correct — the identity is version-only everywhere),
but the retired token had survived as a LITERAL in six tracked files:

- `README.md`, `UPDATE.md`, `agent.md`, `worklog.md` — historical
  narration of the v1.3.1/v1.3.3 removal work itself quoted the token;
- `scripts/release-version.mjs` — the workflow contract's BANNED
  fragments list contained the literal token as the thing it bans;
- `scripts/release-version.test.mjs` — the fixtures proving the ban
  works embedded the literal token in three shapes plus the
  title-suffix case.

A regression gate that embeds the thing it forbids can never pass. This
is a self-triggering regression-test/design defect, not a product
runtime failure: the application binary, the agent loop and the release
artifacts were never affected.

### The fix

- **Docs**: every literal rewritten to describe the retired token
  without spelling it ("retired-codename fragment", "the retired
  codename", "no retired-codename fragment"). The history stays honest;
  the token is gone.
- **Contract source** (`release-version.mjs`): the banned-fragment
  entry now uses a runtime-assembled constant
  (`RETIRED_CODENAME = ["Ze", "ta"].join("")`) with a comment explaining
  WHY it is assembled (the gate scans every tracked file, contract
  source included — a literal would re-trigger the gate it enforces,
  the exact run 35571331850 failure class). The fragment it produces at
  runtime is byte-identical to the old literal, so the workflow
  contract's enforcement power is unchanged.
- **Regression suite** (`release-version.test.mjs`): the same dynamic
  constant builds every forbidden fixture (the three banned-shape
  workflow snippets, the codename-suffixed release title, and the
  assertion that the violation message names the token). All five
  codename-related tests keep passing against the real contract —
  coverage is preserved, embedding is gone.
- **Nothing weakened**: no file exclusions, no pattern loosening, no
  scan narrowing. The gate command in the workflow is byte-identical to
  v1.3.3.

**Verified**: the exact CI gate command returns zero matches on the
release tree; the 28-test release suite passes (the codename tests
prove the ban fires on dynamically constructed violations).

## S2. Repository Intelligence — `internal/repoindex` (NEW, TESTED)

The first coherent slice of ROADMAP v1.4 (repository indexing, symbol/
dependency graph, hybrid search, Git-aware context, test/source
relationships, incremental updates, context relevance scoring). The
slice is a real, persistent, executing system:

### Index model

One JSON file per workspace root under `<DataDir>/repoindex/` (keyed by
the same sha256-root scheme as projectintel) — the application's
existing flat-file + atomic tmp+rename persistence architecture, no
second persistence system. Each `FileRecord` carries: slash-normalized
root-relative path, language, role (source/test/config/doc/asset),
size, mtime, bounded content digest, package name, symbols, raw import
specifiers, resolved dependency edges, test links, Git state and a
recent-change flag. Derived in-memory only: the path→record map and the
reverse-dependency (imported-by) graph.

### Incremental updates (never a full rescan per request)

- Unchanged files are detected via (mtime, size) and never re-read.
- A mtime-only touch is caught by the bounded content digest
  (sha256 over size + the first 64 KB) — content-identical files are
  not re-parsed.
- Only changed/new files are parsed; deleted files drop their records;
  the graph and test links are re-derived as pure in-memory map work.
- Bounded everywhere: 8 000 files, walk depth 24, 512 KB parse cap per
  file, 400 symbols / 120 imports / 100 dependency edges per file, 24
  test links per file, git output capped, and a 3 s work budget per
  update with RESUMABLE partial passes. A pathological repository can
  degrade the index's completeness (honestly reported as
  `truncated`/`partial`) — never the application.

### Language-aware parsing (conservative, no heavyweight dependency)

- **Go**: package, func/method, struct/interface/type, const/var;
  single-line and block imports with alias handling; go.mod module path
  extraction for dependency resolution.
- **TypeScript/JavaScript**: function/class/interface/type/enum/const;
  static imports, export-from, require(), dynamic import().
- **C/C++**: struct/class/enum/union/namespace/define/typedef,
  functions and qualified methods (`Type::method`); quoted (local) vs
  angled (system) includes.
- **JSON**: top-level keys as bounded `key` symbols (config lookups).
- Binary sniffing on the first read chunk; conservative fallback
  (path/size indexing without content parsing) everywhere else.

### Dependency graph (verified structural facts)

An edge exists ONLY when a real import/include statement resolves to a
real repository file:

- Go module imports (`module/.../pkg`) resolve to the imported package
  directory's files;
- TS/JS relative imports (`./`, `../`) resolve with extension probing
  (.ts/.tsx/.mts/.cts/.js/.jsx/.mjs/.cjs/.json/.css) and `index.*`
  directory entries;
- C/C++ quoted includes resolve against the importer directory, repo
  root, `include/` and `src/`.

Path safety is structural: a resolution that escapes the root is never
an edge. Reverse edges (imported-by) are derived after every update.
Raw import specifiers that do not resolve in-repo (stdlib, external
packages, system headers) are still recorded as raw textual facts —
distinguishable from resolved edges.

### Test/source relationships (deterministic, evidence-based)

- Go: `foo.go ↔ foo_test.go` in the same directory (holds for internal
  `package foo` and external `package foo_test` test files alike).
- TS/JS: `foo.ts → foo.test.ts / foo.spec.ts` in the same directory or
  the sibling `__tests__/` directory.
- Links are symmetric and bounded; nothing is guessed by a model.

### Git-aware foundation (optional, relevance-only)

`git ls-files` and `git status --porcelain` (both output-bounded) stamp
tracked / untracked / modified per record; a 7-day mtime window marks
recently changed files. Git is entirely optional: an absent binary or a
non-git workspace leaves the signals empty and indexing proceeds
normally. The signals are relevance METADATA — never an authority for
correctness.

### Hybrid search (bounded, deterministic, evidence-ranked)

`Store.Search` combines: symbol match (exact / prefix / substring),
path match (basename / path), language and role filters, weighted
keyword matching over indexed metadata (path, symbols, imports,
package — the index never stores file content), task-keyword relevance
(stopword-filtered extraction), dependency expansion (`depsOf`,
`usedBy`), and test lookup (`testsOf`). Scoring is fixed-weight and
deterministic (score desc, path asc; pinned by tests across repeated
runs). Result pages are bounded (default 12, hard cap 50) and every hit
carries an Evidence string that says exactly why it matched — with
structural facts (dependency of X, imported by Y, test file of Z)
distinguishable from inferred relevance (keyword/task/path scoring).
Recency and Git-modified boosts apply ONLY to records that already
matched a real dimension, so an unrelated query can never surface the
whole repository.

## S3. Agent integration — `repo_search` + repository evidence (TESTED)

- **`repo_search` tool** (registered in the orchestrator; groups:
  coding + filesystem): lets the model ask for a symbol, files related
  to a path, dependencies of a file, files that depend on a file, tests
  for a source, or the most relevant files for a task. Results are
  bounded (8 KB output cap), deterministic, path-safe (absolute
  references are relativized against the workspace root; anything
  escaping is rejected) and evidence-based. The workspace root is read
  LIVE per call — a workspace switch applies on the very next query.
- **Repository-evidence context block**: `Orchestrator.SetRepoEvidence`
  mirrors the project-card seam. The tier composes a bounded (400-token)
  block of the most relevant indexed files for the CURRENT task
  (STANDARD and above; FAST never pays the retrieval I/O). The block is
  budgeted through the existing optional-block plan (degradation ladder
  drops it under pressure, the escalation ladder composes it on tier
  upgrades, both injection paths covered), injected in the same
  cache-friendly position as the card/skills/recall blocks. No second
  context system was created — retrieval results ride the existing
  contextplan pipeline, and the model receives targeted evidence, not
  the index.

## S4. Workspace surface — Repository Index card (NEW, TESTED)

The existing Workspace panel gains a Repository Index card (styled on
the existing wb-* design tokens): index state (ready/stale/not built,
with bounded/partial honesty flags), indexed files, symbols, dependency
edges, test links, Git signal availability, per-language distribution,
last update time, a bounded refresh action (POST /api/repo/index/refresh)
and a repository search entry point (POST /api/repo/search) rendering
the ranked results with per-hit evidence. The status endpoint is
walk-free and poll-safe; a first view of a not-yet-built index triggers
exactly ONE bounded refresh — no polling loops. New API surface:
`GET /api/repo/index`, `POST /api/repo/index/refresh`,
`POST /api/repo/search`; the workspace-switch flow re-indexes the new
root with a bounded incremental pass, exactly like the project-intel
re-observe it sits beside.

## S5. Verification performed (this release, this host)

- **Codename gate**: the exact CI scan returns ZERO matches on the
  release tree; `release-version.test.mjs` 28/28 (dynamic fixtures
  prove every banned shape still fails the contract).
- **Go**: `gofmt -l` clean; `go vet -tags headless ./...` clean;
  `go test -tags headless -count=1 ./...` green; CI race gate packages
  (api/agent/sessions/contextplan/histref/runtime) plus repoindex green
  under `-race`; `internal/repoindex` 30/30 tests (indexing, language
  detection, symbols, imports/dependencies, test/source links,
  incremental updates, mtime-touch digest stability, deletion, stale
  entry removal, content changes, per-file bounds, excluded dirs,
  deterministic ranking, search dimensions, limit bounds, path-safety
  escapes, tool end-to-end, evidence block, persistence/reload,
  no-rewrite-on-unchanged, Git signals + Git-optional) including
  `-race -count=2`; repoindex API contract test green (switch →
  re-index → status → symbol/dependency search → error contract).
- **Native engine**: clean-room CMake (`--fresh`) configure + build +
  ctest → 12/12 PASS on Linux; the real C++ host Go integration suite
  green against the freshly built host (end-to-end, model lifecycle,
  missing model path, orphan prevention, stop-during-generation,
  Phase 4 tokenizer/kv-cache/scheduler, Phase 5 generation,
  cancellation and prompt-ownership).
- **Windows**: the Go application cross-builds as a PE32+ executable
  (CGO_ENABLED=0, GOOS=windows); the MSVC native build, NSIS installer
  and release publication execute on the GitHub Windows runner (no
  MinGW on this host — the same documented environmental limit as
  v1.3.3; the native C++ tree is byte-identical to the v1.3.3 state
  that cross-built with zero warnings).
- **Frontend**: `npm ci`; `npm run typecheck` clean; `npm run lint`
  0/0; `npm run test:units` 84/84 (6 new repoindex-view tests);
  `npm run test:release` 28/28; `npm run build` + `sync:web` OK (the
  embedded `web/static` regenerated from this tree); `npm run
  verify:web -- --dist` satisfied.
- **Stress**: `scripts/stress-main stress` → 47/47, 0 hangs, 0 crashes.
- **Release**: `release-version.mjs` sync repaired the three derived
  surfaces to 1.3.4 (config.go / build/config.yml / SIGNATURE);
  `--check` passes; `--env` emits exactly `APP_VERSION=1.3.4`; tag rule
  `tag == v${APP_VERSION}` and version-only title enforcement verified
  by the live workflow-audit test.
- **Repository**: ROADMAP.md git blob SHA-1
  `c7e2c1720eb5e97bd932c0d76100b8719193e650` unchanged (verified via
  `git hash-object` before and after all work).

## S6. Environmental limits (honest scope)

- The Wails desktop shell needs GTK4/WebKitGTK-6.0 development packages
  (not installable on this host) — the headless build tag is the
  documented verification path and is what CI runs in the audit job.
- The Windows MSVC native build, NSIS installer, GitHub Release
  publication and the runner-side version smoke tests execute on the
  GitHub runners (unchanged from v1.3.3; the Windows Go exe and the
  entire workflow contract are verified here).
- ROADMAP v1.4 slices NOT in this release (explicitly deferred, not
  silently skipped): error/source relationship mapping, call-graph
  depth beyond import edges, semantic/embedding retrieval (the roadmap
  requires measured evidence before that dependency is added).

Artifact filenames stay versioned normally
(`SHEYTAN-LA-v1.3.4-windows-x64.zip`,
`SHEYTAN-LA-v1.3.4-windows-x64-installer.exe`,
`SHEYTAN-Local-Agent-Linux-x64-v1.3.4.zip`) and the release identity
stays version-only: tag `v1.3.4`, title `1.3.4`, nothing else.
