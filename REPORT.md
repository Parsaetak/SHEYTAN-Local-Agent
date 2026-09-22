# SHEYTAN-Local-Agent v1.3.6 — FINAL FACTUAL REPORT

Regenerated from the ACTUAL final repository state (2026-09-22, deep
repair pass). The previous version of this file described a local-only
commit (`b98b336…`) that never landed and a "NOT pushed" state that has
since changed; that stale content was replaced per the documentation-
truth rule. Every claim below corresponds to an actually executed check
in the producing environment (Linux x86-64, Go 1.26.0, Node 24).
Anything NOT executed there is listed as such.

## Repository state

* Base commit: `57f0c1bc106dae121e0c333892dfd3c9306b7ea7` (`1.3.5`)
* v1.3.6 landed on `main` as `9036e1f` ("v1.3.6", parent `57f0c1b`)
  plus `0207611` ("Remove obsolete Research workspace assets" — the fix
  for failing Actions run `35702334699`, whose root cause was the stale
  tracked frontend files `src/ResearchPanel.tsx` and
  `web/static/assets/ResearchPanel.js`).
* This deep-repair pass is the current HEAD (see git log; committed
  locally with an agent identity — this environment has NO GitHub push
  credentials, so no Actions run could be triggered from here).
* Version: `1.3.6` (package.json is the single canonical source;
  `scripts/release-version.mjs --check` is green: AppVersion,
  productVersion, SIGNATURE all consistent).
* Verified deletion state: `src/ResearchPanel.tsx` and
  `web/static/assets/ResearchPanel.js` do NOT exist in the tree; no
  Research navigation/workspace/panel remains in the frontend; `#research`
  resolves to the Agent layer; `npm run verify:web --dist` is green.

## Engine architecture (verified)

* Managed llama.cpp backend (Chat/Agent → llm.Backend → LlamaBackend →
  LlamaServer → llama-server(.exe) → GGUF) is the primary/default path.
* SHEYTAN native backend (shtn-engine-host → C++ engine core) is a
  separate runtime, isolated from llama.cpp; the REAL host binary was
  built from `native/engine` (make) and smoke-tested over its length-
  prefixed JSON IPC protocol (shutdown op → clean ack, rc 0); all 12
  native test suites pass on Linux.
* LiteRT: no LiteRT implementation exists anywhere in the tree; none
  was invented; nothing claims LiteRT support.

## What this deep-repair pass changed (all with regression tests)

1. §7/§13/§14 — `UpdateEngineNow` is now a FULL deferred-commit
   transaction: stage → validate → atomic swap → verify identity →
   START exact installed binary → verify ready → COMMIT (+cleanup).
   Startup-verification failure now ROLLS BACK to the previous package
   byte-for-byte and returns a NON-NIL error (the old `return msg, nil`
   after a failed start is gone, including the legacy updater path).
   The commit tag/manifest are written only after verified readiness.
2. §11/§12 — `mergeForeignFiles` replaced by allowlisted
   `mergeCompanionFiles` (native engine host + license/metadata only);
   stale DLLs/foreign exes are DROPPED and logged. `copyClosureDedup`
   replaced by `copyEngineClosure` (entry binary + beside-the-binary
   runtime DLLs — including dynamically loaded backends — + metadata;
   never the directory, never neighboring engines' libraries).
3. §15/§16 — cross-process engine ownership added
   (`internal/englease`): lease file + PID + executable path + process
   start-time verification (real OS APIs: OpenProcess/GetProcessTimes
   on Windows, /proc stat starttime on Unix); live foreign holders are
   refused, never killed; stale leases are recoverable only after
   failed verification; the CLI updater refuses to provision under a
   live foreign owner; lifecycle ops hold the owner lease.
4. §17 — adoption now additionally proves ENGINE identity: on-disk
   SHA-256 must match the recorded install manifest, and (when
   provable) the process must have started at/after the recorded
   install time. Missing manifest → explicit "identity check
   unavailable" log, never a fake pass.
5. §9 — Tier-2 system discovery is now wired into production:
   `Rediscover()` (POST `/api/engine/rediscover`) runs the full ladder
   (managed → cache → Tier 1 → bounded Tier-2 scan) and imports a
   validated candidate through the single authority; Tier-2 still never
   runs on the startup path.
6. §19/§20 — launch-phase failure reports now carry ExitCode,
   ExitCodeHex, ExeSHA256/Size/Mtime and the classified failure class
   (never guessed); preflight surfaces `depsUnavailable` /
   `probeUnavailable` reasons in its JSON instead of silently reading
   as "no deps"/"no version".
7. §23/§25 — the canonical data root is now REALLY the install-local
   `<AppRoot>\data` tree: `ResolveRoot()` defaults to
   `<AppRoot>\data` (the installer already created and ACL'd exactly
   this tree; the installer additionally creates `data\workspace`);
   `MigrateAppRootDirectData` folds a pre-`<AppRoot>\data` portable
   layout into the canonical root (rename-first, collision-safe,
   engine bin moved as a unit); explicit `SHEYTAN_DATA_DIR` overrides
   remain authoritative and untouched.
8. §26 — the second search backend (`tools.WebSearch`, DuckDuckGo/Bing
   scraper registered as an agent tool) is REMOVED; web search flows
   exclusively through the internal research service (the SAME service
   behind the Net Search composer control and `/api/net-search`), with
   server-side per-request enforcement and provenance. A regression
   test pins the single-backend contract.
9. §28/§29 — stale scratch manifests deleted (MANIFEST.txt,
   REPLACEMENT-MANIFEST.txt, REPLACEMENT-SHA256.txt,
   UPDATE-MANIFEST.txt — all described superseded or never-landed
   commits); worklog/agent.md stale branch claims corrected; this
   report regenerated from real state. gofmt drift in
   internal/research/service.go fixed.
10. Dead code removed: `downloadEngine`/`copyAll` in internal/updater
    (the last competing download implementation).

## Verification matrix (executed in this environment)

* Frontend: typecheck OK; lint 0 warnings/0 errors; unit tests 96/96;
  release tests 28/28; `npm run build` OK; `verify:web --dist` OK
  (web/static mirrors the current build exactly); release-version
  `--check` OK.
* Go: gofmt clean (whole tree); `go vet -tags headless ./...` OK;
  `go test ./internal/... -tags headless -count=1` → 51/51 packages
  OK; `-race` suites OK (api/agent/sessions/contextplan/histref/
  runtime + llm/updater/englease/proc/config).
* Windows cross-builds from this Linux environment: main desktop
  binary, headless main, cmd/ CLI — all compile (GOOS=windows).
* Native C++: `make` + `make test` in native/engine → 12/12 suites
  pass; real `shtn-engine-host` IPC round-trip verified.
* New regression suites: update-failure semantics + rollback
  (internal/llm), commit-after-verification ordering, DLL/package
  isolation (internal/updater), engine ownership lease
  (internal/englease, incl. live-foreign refusal and stale recovery),
  data-root contract + stray-layout + AppData no-recreation
  (internal/config), single-search-backend (internal/runtime).

## NOT executed in this environment (honest limitations)

* Real Windows runtime acceptance (§32): launch → data root → engine
  discovery → start → PID → /health → model → real inference →
  Stop/Restart — requires a Windows machine; NOT executed here.
* NSIS installer execution and installer/runtime install test to a
  non-AppData location — requires Windows; NOT executed here. The
  installer/runtime agreement was verified by code inspection + tests
  of the shared path contract.
* GPU/NPU acceleration — not verified; report CPU / no verified
  accelerator.
* GitHub Actions run for the final commit — this environment has no
  push credentials; the final commit is local. The failing run
  `35702334699` (commit `9036e1f`) was confirmed failed via the
  Actions page; its root cause (stale tracked frontend files) is
  verified removed in the final tree, and every check that run
  performs is green locally. A push from a credentialed environment
  is required to observe the real run result.
