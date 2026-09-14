// Package updater keeps the bundled libraries current on a schedule.
//
// v1.0.0 introduced scheduled engine updates: the llama.cpp inference
// engine is checked against the upstream GitHub release on a configurable
// cadence — daily, weekly, or monthly — and silently upgraded in the
// background when a newer build exists. Everything degrades gracefully
// offline (checks are skipped and retried on the next due tick) and the
// running engine is restarted around the swap so the user never notices
// more than a brief "engine reloading" caption.
//
// v1.0.3 fixes — the updater was systematically broken before:
//   - "latest release" on GitHub can point at a tag with NO prebuilt
//     binaries (e.g. the v0.3.0 milestone tag), so the updater tried to
//     download an asset that does not exist and every update failed.
//     LatestTag now walks the recent release LIST and returns the newest
//     tag that actually ships a prebuilt asset for this platform.
//   - the manual "Check for engine update now" button was gated by the
//     same due-check as the scheduled pass, so clicking it usually did
//     nothing. CheckAndApplyForced always checks.
//   - swapping the engine files while the engine was RUNNING failed on
//     Windows (the exe is locked). The engine is now stopped before the
//     swap and restarted after.
package updater

import (
        "archive/zip"
        "context"
        "encoding/json"
        "encoding/xml"
        "errors"
        "fmt"
        "io"
        "net/http"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// Schedules supported by the updater.
const (
        ScheduleDaily   = "daily"
        ScheduleWeekly  = "weekly"
        ScheduleMonthly = "monthly"
        ScheduleOff     = "off"
)

// DefaultSchedule is applied when the config value is empty or unknown.
const DefaultSchedule = ScheduleDaily

// DefaultEngineTag is the llama.cpp release the app BUNDLES in its portable
// folder (v1.0.3 ships the engine inside bin/ — no first-run download
// needed). Must match the tag format used by upstream releases.
const DefaultEngineTag = "b10642"

// apiTimeout bounds every network call the updater makes.
const apiTimeout = 20 * time.Second

// Engine is the subset of the llama.cpp manager the updater needs.
type Engine interface {
        IsRunning() bool
        Stop() error
        Restart() error
}

// NormalizeSchedule validates a schedule string, returning the default for
// anything unknown (and "off" passed through untouched).
func NormalizeSchedule(s string) string {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case ScheduleDaily:
                return ScheduleDaily
        case ScheduleWeekly:
                return ScheduleWeekly
        case ScheduleMonthly:
                return ScheduleMonthly
        case ScheduleOff:
                return ScheduleOff
        default:
                return DefaultSchedule
        }
}

// CheckDue reports whether a scheduled update check is due given the last
// check time. "off" never reports due.
func CheckDue(schedule string, last time.Time) bool {
        switch NormalizeSchedule(schedule) {
        case ScheduleOff:
                return false
        case ScheduleDaily:
                return time.Since(last) >= 24*time.Hour
        case ScheduleWeekly:
                return time.Since(last) >= 7*24*time.Hour
        case ScheduleMonthly:
                return time.Since(last) >= 30*24*time.Hour
        }
        return false
}

// stateFile is the shape of installed.json (subset the updater touches).
type stateFile struct {
        AppVersion string               `json:"appVersion"`
        LastRunAt  time.Time            `json:"lastRunAt"`
        Components map[string]component `json:"components"`
}

type component struct {
        Version    string            `json:"version,omitempty"`
        Status     string            `json:"status"`
        ObservedAt time.Time         `json:"observedAt"`
        Meta       map[string]string `json:"meta,omitempty"`
}

// InstalledEngineTag returns the llama.cpp build tag recorded at install or
// last update. Missing/legacy installs report "".
func InstalledEngineTag(cfg *config.Config) string {
        data, err := os.ReadFile(cfg.StatePath())
        if err != nil {
                return ""
        }
        var st stateFile
        if err := json.Unmarshal(data, &st); err != nil {
                return ""
        }
        c, ok := st.Components["llamaServer"]
        if !ok {
                return ""
        }
        if c.Meta != nil {
                if t := c.Meta["engineTag"]; t != "" {
                        return t
                }
        }
        return c.Version
}

// RecordEngineTag persists the engine tag (and install time) into
// installed.json, creating the file when necessary. Failures are logged and
// swallowed — bookkeeping must never break the app.
func RecordEngineTag(cfg *config.Config, tag string) {
        st := stateFile{Components: map[string]component{}}
        if data, err := os.ReadFile(cfg.StatePath()); err == nil {
                _ = json.Unmarshal(data, &st)
        }
        if st.Components == nil {
                st.Components = map[string]component{}
        }
        st.AppVersion = config.AppVersion
        st.LastRunAt = time.Now().UTC()
        c := st.Components["llamaServer"]
        c.Status = "installed"
        c.ObservedAt = time.Now().UTC()
        if c.Meta == nil {
                c.Meta = map[string]string{}
        }
        c.Meta["engineTag"] = tag
        c.Meta["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
        st.Components["llamaServer"] = c
        out, err := json.MarshalIndent(st, "", "  ")
        if err != nil {
                return
        }
        _ = os.MkdirAll(cfg.DataDir, 0o755)
        _ = os.WriteFile(cfg.StatePath(), out, 0o644)
}

// MarkChecked records the last update-check time in the config (persisted by
// the caller).
func MarkChecked(cfg *config.Config) {
        cfg.LastUpdateCheck = time.Now().UTC().Format(time.RFC3339)
}

// ghRelease is the subset of the GitHub release payload we need.
type ghRelease struct {
        TagName string    `json:"tag_name"`
        Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
        Name string `json:"name"`
}

// LatestTag returns the newest llama.cpp release tag that actually ships a
// prebuilt server binary for this OS/arch.
//
// v1.0.3: the previous implementation queried /releases/latest, which can
// point at a milestone tag (v0.3.0) whose release contains ONLY source —
// the download then 404'd and updates never worked. We now page through the
// recent release list and pick the first tag with a matching asset. When
// the API is rate-limited (anonymous GitHub API is capped at 60 req/h per
// IP) we fall back to the releases Atom feed and verify candidate tags by
// HEAD-checking the asset URL.
func LatestTag(ctx context.Context) (string, error) {
        if netcheck.IsOffline() {
                return "", fmt.Errorf("offline — skipping update check")
        }
        if tag, err := latestTagFromAPI(ctx); err == nil && tag != "" {
                return tag, nil
        } else if err != nil && !errors.Is(err, errNoAsset) {
                logging.Default().Warn("updater", "release list via API failed: %v (trying atom feed)", err)
        }
        return latestTagFromAtom(ctx)
}

// errNoAsset marks "API reachable but no release had a matching asset".
var errNoAsset = errors.New("no recent release ships a prebuilt asset for this platform")

// latestTagFromAPI pages the GitHub release list (newest first) and returns
// the first tag whose assets include our platform build.
func latestTagFromAPI(ctx context.Context) (string, error) {
        urls := []string{
                "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=30",
                "https://api.github.com/repos/ggerganov/llama.cpp/releases?per_page=30",
        }
        var lastErr error
        for _, u := range urls {
                ctx, cancel := context.WithTimeout(ctx, apiTimeout)
                req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
                if err != nil {
                        cancel()
                        lastErr = err
                        continue
                }
                req.Header.Set("Accept", "application/vnd.github+json")
                resp, err := http.DefaultClient.Do(req)
                if err != nil {
                        cancel()
                        lastErr = err
                        continue
                }
                var releases []ghRelease
                decodeErr := json.NewDecoder(resp.Body).Decode(&releases)
                _ = resp.Body.Close()
                cancel()
                if decodeErr != nil {
                        lastErr = decodeErr
                        continue
                }
                if resp.StatusCode != 200 {
                        lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
                        continue
                }
                if tag := firstWithAsset(releases); tag != "" {
                        return tag, nil
                }
                lastErr = errNoAsset
        }
        if lastErr == nil {
                lastErr = errNoAsset
        }
        return "", lastErr
}

var _ = fmt.Sprintf // keep fmt when stubs are trimmed

// AssetInfo is the exported asset shape (name only) used by tests.
type AssetInfo = ghAsset

// GHAssetStub is kept for source compatibility; use AssetInfo.
type GHAssetStub = ghAsset

// ReleaseInfo is the exported shape of one GitHub release (tag + asset
// names) used by FirstWithAsset. Test-facing alias (v1.0.3).
type ReleaseInfo = ghRelease

// GHReleaseStub is kept for source compatibility; use ReleaseInfo.
type GHReleaseStub = ghRelease

// FirstWithAsset is the exported test seam for firstWithAsset: it walks a
// newest-first release list and returns the first tag whose assets include
// the prebuilt asset for this platform.
func FirstWithAsset(releases []ghRelease) string {
        return firstWithAsset(releases)
}

// firstWithAsset walks a newest-first release list and returns the first
// tag carrying the platform asset (v1.0.3).
func firstWithAsset(releases []ghRelease) string {
        for _, r := range releases {
                if r.TagName == "" {
                        continue
                }
                want := AssetName(r.TagName)
                if want == "" {
                        continue
                }
                for _, a := range r.Assets {
                        if a.Name == want {
                                return r.TagName
                        }
                }
        }
        return ""
}

// atomFeed is the subset of the GitHub releases Atom feed we need.
type atomFeed struct {
        Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
        Title string `xml:"title"`
}

// latestTagFromAtom parses the public releases Atom feed (no rate limit)
// and HEAD-checks candidate tags until one has a downloadable asset.
func latestTagFromAtom(ctx context.Context) (string, error) {
        ctx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()
        req, err := http.NewRequestWithContext(ctx, http.MethodGet,
                "https://github.com/ggml-org/llama.cpp/releases.atom", nil)
        if err != nil {
                return "", err
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
                return "", fmt.Errorf("atom feed: HTTP %d", resp.StatusCode)
        }
        var feed atomFeed
        if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
                return "", err
        }
        checked := 0
        for _, e := range feed.Entries {
                tag := strings.TrimSpace(e.Title)
                if tag == "" || !strings.HasPrefix(tag, "b") {
                        continue // skip milestone tags (vX.Y.Z) — they carry no binaries
                }
                if AssetURL(tag) == "" {
                        continue
                }
                checked++
                if checked > 10 {
                        break // bounded effort
                }
                if assetExists(ctx, tag) {
                        return tag, nil
                }
        }
        return "", fmt.Errorf("no recent release ships a prebuilt asset for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// AssetName returns the prebuilt llama.cpp asset name for this OS/arch at
// the given tag, or "" when no prebuilt asset exists for the platform.
func AssetName(tag string) string {
        var arch string
        switch runtime.GOARCH {
        case "amd64":
                arch = "x64"
        case "arm64":
                arch = "arm64"
        default:
                return ""
        }
        switch runtime.GOOS {
        case "windows":
                return fmt.Sprintf("llama-%s-bin-win-cpu-%s.zip", tag, arch)
        case "darwin":
                return fmt.Sprintf("llama-%s-bin-macos-%s.zip", tag, arch)
        case "linux":
                return fmt.Sprintf("llama-%s-bin-ubuntu-%s.zip", tag, arch)
        }
        return ""
}

// AssetURL builds the download URL for the prebuilt asset at a tag.
func AssetURL(tag string) string {
        a := AssetName(tag)
        if a == "" {
                return ""
        }
        return "https://github.com/ggml-org/llama.cpp/releases/download/" + tag + "/" + a
}

// assetExists HEAD-checks the prebuilt asset URL for a tag (v1.0.3).
func assetExists(ctx context.Context, tag string) bool {
        return AssetExists(ctx, tag)
}

// AssetExists is the exported test/production seam for assetExists: it
// HEAD-checks whether the prebuilt engine asset for this platform exists
// at the given tag. v1.1.5Z repair: the engine download path needs this to
// probe the pinned tag before falling back to a release scan, because
// llama.cpp removed its Linux prebuilt assets upstream (the pinned tag
// now 404s on Linux while the Windows asset still exists).
func AssetExists(ctx context.Context, tag string) bool {
        url := AssetURL(tag)
        if url == "" {
                return false
        }
        checkCtx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()
        req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
        if err != nil {
                return false
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return false
        }
        _ = resp.Body.Close()
        // GitHub answers 302 → 200 for existing release assets.
        return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// engineBinaryName is the local name of the server binary.
func engineBinaryName() string {
        if runtime.GOOS == "windows" {
                return "llama-server.exe"
        }
        return "llama-server"
}

// UpdateEngine downloads the engine at `tag` and swaps it in. When `eng` is
// non-nil and running, the engine is STOPPED for the swap (Windows locks a
// running exe) and restarted afterwards. Returns the human-readable outcome.
func UpdateEngine(ctx context.Context, cfg *config.Config, eng Engine, tag string) (string, error) {
        url := AssetURL(tag)
        if url == "" {
                return "", fmt.Errorf("no prebuilt llama.cpp asset for %s/%s", runtime.GOOS, runtime.GOARCH)
        }
        binDir := filepath.Join(cfg.DataDir, "bin")
        if cfg.LlamaBinPath != "" {
                binDir = filepath.Dir(cfg.LlamaBinPath)
        }
        if err := os.MkdirAll(binDir, 0o755); err != nil {
                return "", err
        }

        wasRunning := eng != nil && eng.IsRunning()

        // v1.0.3: stop the engine BEFORE touching files — on Windows the
        // running llama-server.exe is locked and every copy over it fails
        // (the old updater only restarted after the swap and silently died).
        if wasRunning {
                logging.Default().Info("updater", "stopping engine for update")
                if err := eng.Stop(); err != nil {
                        return "", fmt.Errorf("stop engine before update: %w", err)
                }
                // Give the OS a beat to release file handles and the port.
                time.Sleep(700 * time.Millisecond)
        }

        logging.Default().Info("updater", "downloading engine %s from %s", tag, url)
        if err := downloadEngine(ctx, url, tag, binDir); err != nil {
                // Best effort: bring the old engine back up.
                if wasRunning {
                        _ = eng.Restart()
                }
                return "", fmt.Errorf("download engine %s: %w", tag, err)
        }
        RecordEngineTag(cfg, tag)

        if wasRunning {
                logging.Default().Info("updater", "restarting engine with %s", tag)
                if err := eng.Restart(); err != nil {
                        return fmt.Sprintf("engine updated to %s but restart failed: %v", tag, err), nil
                }
        }
        return fmt.Sprintf("engine updated to llama.cpp %s", tag), nil
}

// CheckAndApply performs one scheduled check: when due, online, and a newer
// tag exists, the engine is updated. The caller persists cfg
// (LastUpdateCheck) after the call.
func CheckAndApply(ctx context.Context, cfg *config.Config, eng Engine) (msg string, updated bool, err error) {
        return checkAndApply(ctx, cfg, eng, false)
}

// CheckAndApplyForced performs one check NOW, ignoring the schedule and the
// last-check timestamp (the "Check for engine update now" button,
// v1.0.3 — the button used to hit the due-gate and do nothing).
func CheckAndApplyForced(ctx context.Context, cfg *config.Config, eng Engine) (msg string, updated bool, err error) {
        return checkAndApply(ctx, cfg, eng, true)
}

func checkAndApply(ctx context.Context, cfg *config.Config, eng Engine, force bool) (msg string, updated bool, err error) {
        schedule := NormalizeSchedule(cfg.UpdateSchedule)
        if schedule == ScheduleOff && !force {
                return "updates off", false, nil
        }
        last, _ := time.Parse(time.RFC3339, cfg.LastUpdateCheck)
        if !force && !CheckDue(schedule, last) {
                return "up to date (last check " + humanWhen(last) + ")", false, nil
        }
        MarkChecked(cfg)
        if netcheck.IsOffline() {
                return "offline — update check skipped", false, nil
        }
        latest, err := LatestTag(ctx)
        if err != nil {
                return "check failed: " + err.Error(), false, err
        }
        current := InstalledEngineTag(cfg)
        if current == "" {
                current = DefaultEngineTag
        }
        if latest == current {
                return fmt.Sprintf("engine is current (%s)", latest), false, nil
        }
        msg, err = UpdateEngine(ctx, cfg, eng, latest)
        if err != nil {
                return msg, false, err
        }
        logging.Default().Info("updater", "%s", msg)
        return msg, true, nil
}

// RunScheduled is the long-lived background loop: an immediate pass at boot
// (if due) then a re-evaluation every 6 hours so daily/weekly/monthly
// cadences fire even in long-running sessions. `notify` (optional) receives
// human-readable status lines; `save` persists the config.
//
// v1.1.4Z: takes the live config Source. CheckAndApply mutates cfg
// (LastUpdateCheck) in place, so each pass runs on a PRIVATE copy that is
// published back through the source — a published immutable value is never
// mutated. Wired by api.Server.EnsureSetup (previously zero callers).
//
// v1.2.0 (deterministic shutdown): returns a done channel closed when the
// loop has observed ctx cancellation AND any in-flight pass (including a
// download/extract) has finished. The owner MUST cancel the context and
// may wait on the channel — that is the whole ownership contract that
// previously leaked an active updater goroutine past Server.Close and
// raced test temp-directory cleanup with .update-stage writes.
func RunScheduled(ctx context.Context, src *config.Source, eng Engine, notify func(string), save func()) <-chan struct{} {
        if notify == nil {
                notify = func(string) {}
        }
        if save == nil {
                save = func() {}
        }

        done := make(chan struct{})

        go func() {
                defer close(done)

                pass := func() {
                        cur := src.Load()
                        mutable := *cur

                        msg, updated, err := CheckAndApply(ctx, &mutable, eng)

                        src.Store(&mutable)

                        if err != nil {
                                logging.Default().Warn("updater", "%s", msg)
                        } else if updated {
                                logging.Default().Info("updater", "%s", msg)
                        }
                        notify(msg)
                        save()
                }

                if ctx.Err() == nil {
                        pass()
                }

                t := time.NewTicker(6 * time.Hour)
                defer t.Stop()

                for {
                        select {
                        case <-ctx.Done():
                                return
                        case <-t.C:
                                pass()
                        }
                }
        }()

        return done
}

// downloadEngine fetches the release zip for `tag` and extracts only the
// server binary (plus adjacent runtime DLLs) into binDir, staging first so
// a bad download can never destroy a working engine.
//
// v1.1.4Z: the transfer is capped (engineUpdateCapBytes) — the previous
// io.Copy accepted an arbitrarily large body.
const engineUpdateCapBytes = 2 << 30 // 2 GiB; Vulkan bundles are ~300 MB

func downloadEngine(ctx context.Context, url, tag, binDir string) error {
        ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
        defer cancel()
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return err
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
                return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
        }
        tmp, err := os.CreateTemp("", "llama-update-*")
        if err != nil {
                return err
        }
        tmpPath := tmp.Name()
        defer os.Remove(tmpPath)

        written, err := io.Copy(tmp, io.LimitReader(resp.Body, engineUpdateCapBytes+1))
        if err != nil {
                tmp.Close()
                return err
        }
        if written > engineUpdateCapBytes {
                tmp.Close()
                return fmt.Errorf("engine download exceeds %d bytes", engineUpdateCapBytes)
        }
        tmp.Close()

        stage := filepath.Join(binDir, ".update-stage")
        _ = os.RemoveAll(stage)
        if err := extractZip(tmpPath, stage); err != nil {
                _ = os.RemoveAll(stage)
                return err
        }
        found := findBinary(stage)
        if found == "" {
                _ = os.RemoveAll(stage)
                return fmt.Errorf("release zip for %s contained no server binary", tag)
        }
        _ = os.RemoveAll(filepath.Join(binDir, ".update-old"))
        if err := copyAll(filepath.Dir(found), binDir); err != nil {
                _ = os.RemoveAll(stage)
                return err
        }
        _ = os.RemoveAll(stage)
        _ = os.Chmod(filepath.Join(binDir, engineBinaryName()), 0o755)
        return nil
}

// findBinary locates the server executable inside an extracted tree.
func findBinary(root string) string {
        var hit string
        _ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
                if err != nil || hit != "" || info == nil {
                        return nil
                }
                if info.IsDir() {
                        return nil
                }
                n := strings.ToLower(info.Name())
                if n == "llama-server.exe" || n == "llama-server" {
                        hit = p
                }
                return nil
        })
        return hit
}

func copyAll(srcDir, dstDir string) error {
        return filepath.Walk(srcDir, func(p string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                rel, _ := filepath.Rel(srcDir, p)
                target := filepath.Join(dstDir, rel)
                if info.IsDir() {
                        return os.MkdirAll(target, 0o755)
                }
                data, err := os.ReadFile(p)
                if err != nil {
                        return err
                }
                if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
                        return err
                }
                return os.WriteFile(target, data, info.Mode())
        })
}

// extractZip extracts a zip archive into dir.
//
// v1.1.4Z: every member path is sanitized (no absolute paths, volume
// names, or .. traversal) and per-entry sizes are bounded. The previous
// version joined filepath.Join(dir, f.Name) with NO validation — a
// hostile archive could write outside the staging directory.
const engineUpdateEntryCapBytes = 2 << 30 // single-member cap

func extractZip(zipPath, dir string) error {
        zr, err := zip.OpenReader(zipPath)
        if err != nil {
                return err
        }
        defer zr.Close()
        if err := os.MkdirAll(dir, 0o755); err != nil {
                return err
        }
        for _, f := range zr.File {
                out, err := safeZipPath(dir, f.Name)
                if err != nil {
                        return err
                }
                if f.FileInfo().IsDir() {
                        if err := os.MkdirAll(out, 0o755); err != nil {
                                return err
                        }
                        continue
                }
                if f.UncompressedSize64 > engineUpdateEntryCapBytes {
                        return fmt.Errorf("zip member %q exceeds %d bytes", f.Name, engineUpdateEntryCapBytes)
                }
                if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
                        return err
                }
                rc, err := f.Open()
                if err != nil {
                        return err
                }
                fo, err := os.Create(out)
                if err != nil {
                        _ = rc.Close()
                        return err
                }
                _, copyErr := io.Copy(fo, io.LimitReader(rc, engineUpdateEntryCapBytes+1))
                closeErr := fo.Close()
                rcErr := rc.Close()
                if copyErr != nil {
                        return copyErr
                }
                if closeErr != nil {
                        return closeErr
                }
                if rcErr != nil {
                        return rcErr
                }
        }
        return nil
}

// safeZipPath validates one archive member and returns the absolute
// extraction target.
//
// v1.2.0: THE single authoritative safe archive-path validator —
// internal/llm delegates here so the engine updater and the app updater
// can never disagree about what an extractable member looks like.
//
// The validator is separator-canonical: the member name is first viewed
// with every backslash as a separator, so the checks hold regardless of
// which OS the archive was created on. A member is accepted only when it
// is a RELATIVE, CLEAN path that stays inside the extraction root.
// Rejected:
//   - POSIX-absolute members          "/absolute/path"
//   - Windows-rooted members          "\absolute\path" (→ "/absolute/path")
//   - drive-letter members            "C:\x" / "C:/x"
//   - UNC members                     "\\server\share\path"
//   - any ".." traversal              "../x", "..\x", "foo/../../x",
//     mixed separators "..\/x"
//   - empty, "." or NUL-bearing names
//
// The Windows-specific trap this fixes: "\absolute\path" is NOT absolute
// under filepath.IsAbs on Windows (it is "rooted" without a volume), so
// the previous IsAbs/VolumeName checks alone let it through.
func safeZipPath(dir, name string) (string, error) {
        name = strings.TrimSpace(name)

        if name == "" {
                return "", fmt.Errorf("zip contains an empty path")
        }

        if strings.ContainsRune(name, 0) {
                return "", fmt.Errorf("zip path %q contains a NUL byte", name)
        }

        // Separator-canonical view: backslash becomes a separator on every
        // host, so rooted/UNC/traversal shapes are visible everywhere.
        unified := strings.ReplaceAll(name, "\\", "/")

        // POSIX-absolute, Windows-rooted ("\x") and UNC ("\\srv/share")
        // members all begin with a separator once unified.
        if strings.HasPrefix(unified, "/") {
                return "", fmt.Errorf("zip path %q is absolute", name)
        }

        // Drive-letter volumes: "C:/x" and "C:\x".
        if len(unified) >= 2 && unified[1] == ':' {
                return "", fmt.Errorf("zip path %q contains a volume", name)
        }

        if unified == "." || unified == "" {
                return "", fmt.Errorf("zip contains invalid path %q", name)
        }

        clean := filepath.Clean(filepath.FromSlash(unified))

        if clean == "." || clean == string(filepath.Separator) || clean == "" {
                return "", fmt.Errorf("zip contains invalid path %q", name)
        }

        // Belt and braces for host-native spellings that survive the
        // unified view (device namespaces, reserved DOS forms).
        if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
                return "", fmt.Errorf("zip path %q is absolute or contains a volume", name)
        }

        base, err := filepath.Abs(dir)
        if err != nil {
                return "", err
        }

        base = filepath.Clean(base)

        target := filepath.Join(base, clean)
        rel, err := filepath.Rel(base, target)
        if err != nil {
                return "", err
        }

        if rel == "" || rel == ".." ||
                strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
                return "", fmt.Errorf("zip path %q escapes extraction directory", name)
        }

        return target, nil
}

// SafeArchivePath is the exported view of the authoritative validator for
// sibling packages (internal/llm engine downloads share the contract).
func SafeArchivePath(dir, name string) (string, error) {
        return safeZipPath(dir, name)
}

func humanWhen(t time.Time) string {
        if t.IsZero() {
                return "never"
        }
        d := time.Since(t)
        switch {
        case d < time.Hour:
                return fmt.Sprintf("%d min ago", int(d.Minutes()))
        case d < 24*time.Hour:
                return fmt.Sprintf("%d h ago", int(d.Hours()))
        default:
                return fmt.Sprintf("%d d ago", int(d.Hours()/24))
        }
}
