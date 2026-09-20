# UPDATE.md — v1.3.0 Runtime Structure, UX & GitHub Workflow Upgrade

**Release:** `v1.3.0` (canonical application version; single version hierarchy:
package.json → release-version.mjs → config.go / build/config.yml / SIGNATURE)
**Base:** `main @ 24aa79b` (`v1.2.9`) · **Date:** 2026-09-20
**Package:** `SHEYTAN-Local-Agent-v1.3.0-UPDATE.zip`
**ROADMAP.md:** byte-identical to the locked baseline (git blob SHA-1
`c7e2c1720eb5e97bd932c0d76100b8719193e650`) — verified before and after
all work.

This release upgrades the v1.2.9 tree in six connected areas:
(1) runtime filesystem correctness (the malformed `%LOCALAPPDATA%` paths),
(2) clean startup logging, (3) reliable scrolling on every workspace tab,
(4) a professional, user-outcome-oriented Settings surface, (5) a
first-class GitHub clone workflow with automatic workspace switching, and
(6) regression coverage for all of it. The four-layer architecture
(transcript / summary / retrieval / artifacts), the run transport and the
engine lifecycle are unchanged — this is not a rewrite; every fix targets
a verified root cause.

---

## S1. Runtime path correctness (FIXED, TESTED)

- DEFECT: the v1.2.9 NSIS installer declared the data location as a
  machine-level `REG_EXPAND_SZ` value carrying the literal string
  `%LOCALAPPDATA%\SHEYTAN-LA`. Machine environment values are expanded
  before per-user variables exist, so the application received the LITERAL
  string; because it is not an absolute path, every join anchored it to
  the working directory and the runtime created real directories shaped
  like `<root>\%LOCALAPPDATA%\SHEYTAN-LA\models`.
- FIX (source): the installer now writes a plain `REG_SZ` absolute path,
  expanding `$LOCALAPPDATA` at install time
  (packaging/nsis/installer.nsi).
- FIX (runtime): `internal/config/paths.go` is the ONE authoritative
  resolver — environment references expand exactly once; a value that
  still contains an unresolved token (or a doubled product nesting) is
  REJECTED to the canonical application root with a reported reason;
  relative values anchor at the executable root, never the CWD; models,
  sessions, logs and workspace derive from the canonical root. Load-time
  normalization guarantees no subsystem can observe a raw `%...%` path.
- Tests: `internal/config/paths_test.go` (expansion, rejection, CWD
  independence, startup directory contract, persisted-config rejection).

## S2. Malformed-root migration (IMPLEMENTED, TESTED)

- `internal/config/migrate.go` folds v1.2.9 malformed trees
  (`<root>\%LOCALAPPDATA%\SHEYTAN-LA\...`, doubled SHEYTAN-LA nesting)
  into the canonical root: files are renamed when the destination is
  free, hash-verified copies otherwise; a collision keeps the NEWER file
  on either side; a recovered config.json is re-loaded (or preserved as
  `config.legacy.json` when both trees carry one); the malformed source
  tree is removed only after every entry is accounted for.
- Idempotent and restart-safe: an interrupted pass leaves the source
  partially intact and the next boot completes it; re-running the merge
  is a no-op.
- The migration reports every decision to the log with actionable
  context, rendered so the literal token never appears in runtime output.
- Tests: `internal/config/migrate_test.go` (token tree, doubled nesting,
  idempotency, restart resume, newer-wins collisions, config recovery,
  healthy roots untouched) plus a REAL-BINARY acceptance run
  (`scripts/` migration harness): fresh boot against a synthetic v1.2.9
  malformed installation migrates models/sessions/config, removes the
  token tree, boots v1.3.0 on the recovered configuration, and a second
  boot is a verified no-op.

## S3. Clean logging (FIXED, TESTED)

- DEFECT: `fastSnapshot()` logged INFO on EVERY call, and every UI
  surface (environment endpoint, System panel, perf poll) calls it — one
  session produced dozens of identical `fast snapshot in 0 ms` lines.
- FIX: routine reads are silent; ONE concise `fast environment ready`
  summary per process; the deep probe keeps its single
  `deep probe completed in X ms (sources: …)` measurement; the
  per-source probe lines moved to DEBUG.
- DEFECT: the scheduled updater could log `WARN [updater]` with an empty
  message (UpdateEngine returns `("", err)` on download failures).
- FIX: the updater call sites always carry the error detail, and the log
  manager now upgrades any blank WARN/ERROR to a record naming the
  emitting call site — an empty warning is structurally impossible.
- Tests: `internal/sysinfo/logging_v130_test.go` (25 concurrent
  ProbeFast calls produce exactly one summary and zero per-call lines),
  `internal/logging/blankwarn_v130_test.go`.

## S4. Universal workspace scrolling (FIXED, BROWSER-VALIDATED)

- DEFECT: `.view-transition` was a plain block under four
  `overflow: hidden` ancestors; its content-height flex basis overflowed
  `.workspace` invisibly — the System tab (and tall Workspace/Research
  content) had NO scrollable viewport anywhere.
- FIX: ONE layout contract — `.workspace > .view-transition` becomes a
  shrinkable flex column; System, Workspace, Research and Settings own a
  single vertical scroll viewport (wheel, trackpad, scrollbar, keyboard
  with focus inside); Agent and Coding Lab keep their existing internal
  IDE-style scroll regions (conversation stream, task list, lab detail)
  which now engage correctly. The per-tab `:has()` special case is
  deleted — one rule covers every tab.
- Validation: every tab measured in a real browser (scrollHeight vs
  clientHeight + programmatic scrollTop with tall injected content),
  short-window (480 px) re-validated, per-tab evidence captured.
- Tests: `src/scroll-contract.test.ts` pins the contract in the shipped
  CSS.

## S5. Professional Settings (IMPLEMENTED, TESTED, BROWSER-VALIDATED)

- Eight sections replace the ten implementation-oriented tabs: General,
  Models, Performance (Quiet / Balanced / Maximum postures + measured
  context/metrics), Agent & Tools, Network, Updates, Diagnostics
  (read-only) and Advanced.
- Users choose outcomes; SHEYTAN chooses implementation details: the MM
  projector is "Automatically managed" (detected → matched → loaded →
  verified by the backend; the raw path field is gone from normal UI —
  the expert override lives in Advanced); engine host/port/binary/flags,
  cache tuning, batch/thread controls, sandbox resource caps and storage
  limits are Advanced-only; compatibility level/reason remain read-only
  measured facts.
- Backward compatible: every legacy config field still loads and saves
  (the backend struct is unchanged); tests pin that ordinary saves
  cannot erase unexposed fields.
- Save-safety fixes: `llm` patches now deep-merge field-by-field (the
  v1.2.9 shallow replace silently zeroed sampling fields on posture and
  preset applies); posture applies write the real top-level `ubatchSize`
  (the v1.2.9 apply wrote a phantom `llm.ubatchSize`); hardware facts
  load on Diagnostics too.
- Tests: `src/settings-sections.test.ts` (structure contract),
  `internal/api/config_patch_v130_test.go` (deep merge, legacy field
  preservation, API-key redaction round-trip).

## S6. First-class GitHub cloning (IMPLEMENTED, TESTED, END-TO-END)

- `internal/gitclone`: URL validation (https, `.git`, bare-host and SSH
  forms; credentials/ports/queries rejected), destination validation
  (non-empty unrelated directories refused), structured clone execution
  through the validated `internal/proc` seam — explicit argument vector,
  no shell, tree-kill cancellation, bounded captured output, hard
  timeout, non-interactive git env, post-clone verification
  (`git rev-parse HEAD`), and classified failures with actionable
  messages (git-unavailable, invalid-url, auth, not-found, network,
  destination-exists, permission, canceled, timeout).
- `internal/api/clone.go`: `POST /api/workspace/clone`,
  `GET /api/workspace/clone/status`, `POST /api/workspace/clone/cancel`
  — the v1.2.3 Download-Manager job pattern (single slot, immediate
  first snapshot, ~1.2 s polling). On success the workspace switches
  through the EXACT `/api/workspace/switch` sequence (tool rebind →
  project intel → config persist), skipped honestly if a run is active.
- Workspace tab UI: Clone card (URL, optional destination/branch,
  progress bar, cancel, classified errors with git output under
  "details", success with HEAD commit and the switched workspace).
- Tests: `internal/gitclone/clone_test.go` (fake-git process matrix),
  `internal/api/clone_api_test.go` (HTTP contract incl. auto-switch),
  `src/clone-url.test.ts` (client pre-validation), plus a REAL
  end-to-end clone of `github.com/octocat/Hello-World` through the
  browser UI with verified workspace switch.

## Verification (this host; measured)

- Go: `go test -tags headless -count=1 ./...` PASS (full tree);
  `go vet -tags headless ./...` clean; `gofmt` clean;
  `GOOS=windows CGO_ENABLED=0 go build ./...` PASS (full Windows
  cross-compile); focused `-race` gate on the concurrency-heavy
  packages PASS.
- Frontend: `npm ci`; `tsc --noEmit` clean; `oxlint` 0/0;
  `node --test` 78/78 (68 prior + 10 new); `vite build` + `sync-web` OK.
- Version: `node scripts/release-version.mjs --check` PASS (canonical
  1.3.0 everywhere).
- Runtime acceptance: real-binary malformed-path migration (boot 1
  migrates + recovers config; boot 2 no-op); clean-startup log
  inspection (exactly one fast-environment summary, zero per-call
  snapshots, zero blank warnings); per-tab scroll validation in a real
  browser; live GitHub clone + automatic workspace switch.
- NOT RUN HERE: the Windows desktop (Wails/WebView2) build and native
  installer packaging — this host is Linux; CI owns the Windows matrix.

## Known limitations

- Cloning private repositories over HTTPS depends on the user's Git
  credential manager; over SSH, on the user's loaded keys (GIT_TERMINAL_PROMPT=0
  fails fast with a classified authentication error rather than hanging).
- The clone progress percentage derives from git's `--progress` output;
  repository phases without percentage lines advance the phase label but
  hold the bar.
- The Windows-specific runtime checks (§14 items 1–12 on Windows) were
  validated on Linux (headless runtime + browser) and Windows
  cross-compilation; a Windows host run is the remaining step CI owns.
