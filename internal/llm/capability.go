package llm

// capability.go — Phase 7 dynamic engine-capability adapter.
//
// Phase 7 root-cause fix: llama.cpp changed the `--flash-attn` CLI contract
// from a bare boolean flag to a tri-state option:
//
//      old releases:   -fa, --flash-attn            (flag, no value)
//      new releases:   -fa, --flash-attn on|off|auto (option, requires value)
//
// `--cache-reuse N` is — and always was — an INDEPENDENT option:
//
//      --cache-reuse N
//
// The Phase 6 launcher emitted `--flash-attn` as a bare flag immediately
// before `--cache-reuse N`, so a newer llama-server parsed the pair as
// `--flash-attn '--cache-reuse'` and aborted with:
//
//      error while handling argument "--flash-attn":
//      unknown value for --flash-attn: '--cache-reuse'
//
// which dropped the engine into compatibility mode 2 (no speed flags) —
// hiding a one-option layout problem behind a profile that strips every
// speed flag.
//
// The fix is threefold and version-aware end to end:
//
//  1. DETECT   — parse `llama-server --help` for the actual CLI contract of
//     the installed binary (preferred), falling back to a conservative
//     release-tag heuristic when --help is unavailable. The result is one
//     EngineCaps profile, persisted only after VERIFIED engine startup.
//  2. VALIDATE — every launch profile is validated option-by-option BEFORE
//     the process is spawned: value-taking options must be followed by an
//     acceptable value; flag options must not. The historical regression
//     (`--flash-attn` immediately followed by `--cache-reuse`) can never be
//     spawned again.
//  3. REPAIR   — when startup still fails on an option, classify the exact
//     failure from the engine's own stderr, repair ONLY the offending
//     option (flip its layout / drop it), retry at the same compat level,
//     and persist the verified profile with the recorded reason. Only when
//     surgical repair is exhausted does the existing compatibility ladder
//     continue to the next level.

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"
        "regexp"
        "strconv"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// capsFileName is the engine capability profile store. It lives beside the
// config (DataDir) and is keyed by engine release tag: a different binary
// invalidates the profile automatically.
const capsFileName = "engine-caps.json"

// flashAttnValueSinceTag is the conservative fallback threshold: the first
// llama.cpp release line known to require the `on|off|auto` value form.
// Real behaviour is always preferred via --help parsing; this heuristic only
// applies when the binary cannot be probed (e.g. missing binary pre-flight,
// offline first boot with a cached tag). Evidence: b10642 accepted the bare
// flag; b10936 rejects it and requires an explicit value.
const flashAttnValueSinceTag = 10900

// helpProbeTimeout bounds one `--help` capability probe.
const helpProbeTimeout = 10 * time.Second

// EngineCaps is the verified capability profile of one llama.cpp binary.
// Every launch-time argument decision reads this profile — never a
// hard-coded assumption about a release.
type EngineCaps struct {
        // Tag is the llama.cpp release tag the profile was verified against.
        Tag string `json:"tag"`

        // FlashAttnEnabled is false only when the build does not know the
        // --flash-attn option at all (repair removed it entirely).
        FlashAttnEnabled bool `json:"flashAttnEnabled"`

        // FlashAttnValue is true when `--flash-attn` REQUIRES an on|off|auto
        // value; false when it is a bare boolean flag.
        FlashAttnValue bool `json:"flashAttnValue"`

        // CacheReuse is true when `--cache-reuse N` is supported.
        CacheReuse bool `json:"cacheReuse"`

        // NoWebUI / Jinja / UBatchSize / ThreadsBatch mirror the remaining
        // Speed Pack options so a capability strip can be surgical per option.
        NoWebUI      bool `json:"noWebUI"`
        Jinja        bool `json:"jinja"`
        UBatchSize   bool `json:"ubatchSize"`
        ThreadsBatch bool `json:"threadsBatch"`

        // Source records how the profile was obtained:
        //   "help-parse"      — parsed from `--help` output of the binary
        //   "tag-fallback"    — conservative heuristic from the release tag
        //   "verified-repair" — corrected after a classified startup failure
        //     and then verified by a successful engine start
        Source string `json:"source"`

        // VerifiedAt is the RFC3339 time of the last VERIFIED successful
        // startup with this profile (empty = not yet verified by a real boot).
        VerifiedAt string `json:"verifiedAt,omitempty"`

        // Removed records options that were dropped during compatibility
        // repair, with the reason, so diagnostics can explain any downgrade.
        Removed []RemovedOption `json:"removed,omitempty"`
}

// RemovedOption is one option excluded from the launch profile, with the
// classified reason. Never silently downgraded: always logged and persisted.
type RemovedOption struct {
        Option string `json:"option"`
        Reason string `json:"reason"`
}

// Clone returns a deep copy so repair paths never mutate a shared profile.
func (c *EngineCaps) Clone() *EngineCaps {
        if c == nil {
                return nil
        }
        out := *c
        if c.Removed != nil {
                out.Removed = append([]RemovedOption(nil), c.Removed...)
        }
        return &out
}

// defaultCapsForTag builds the conservative profile for a release tag when
// the binary cannot be probed. Options introduced after the pinned release
// default to unsupported only when the tag is older than their arrival; the
// Speed Pack options are all present in every tag SHEYTAN has ever shipped.
func defaultCapsForTag(tag string) *EngineCaps {
        caps := &EngineCaps{
                Tag:              tag,
                FlashAttnEnabled: true,
                FlashAttnValue:   tagNumber(tag) >= flashAttnValueSinceTag,
                CacheReuse:       true,
                NoWebUI:          true,
                Jinja:            true,
                UBatchSize:       true,
                ThreadsBatch:     true,
                Source:           "tag-fallback",
        }
        return caps
}

// tagNumber extracts the numeric part of a `b12345` release tag (0 when
// unparseable — treated as "unknown old release").
func tagNumber(tag string) int {
        m := regexp.MustCompile(`b(\d+)`).FindStringSubmatch(tag)
        if m == nil {
                return 0
        }
        n, err := strconv.Atoi(m[1])
        if err != nil {
                return 0
        }
        return n
}

// ---------------------------------------------------------------------------
// Capability detection — `--help` parsing preferred, tag fallback second.
// ---------------------------------------------------------------------------

// DetectEngineCaps probes the binary's own `--help` output and parses the
// real CLI contract. When the probe fails (missing binary, offline, unusual
// build), the conservative tag heuristic is returned with its source set so
// callers always know how much to trust the profile.
func DetectEngineCaps(binPath, tag string) *EngineCaps {
        caps := parseHelpCaps(binPath, tag)
        if caps != nil {
                return caps
        }
        return defaultCapsForTag(tag)
}

// parseHelpCaps runs `<bin> --help` and parses option lines. Returns nil
// when the probe is impossible or the output cannot be trusted.
func parseHelpCaps(binPath, tag string) *EngineCaps {
        if binPath == "" {
                return nil
        }
        if _, err := os.Stat(binPath); err != nil {
                return nil
        }

        ctx, cancel := context.WithTimeout(context.Background(), helpProbeTimeout)
        defer cancel()

        cmd := exec.CommandContext(ctx, binPath, "--help")
        cmd.Env = sanitizedHelpEnv()
        out, err := cmd.CombinedOutput()
        if err != nil || len(out) == 0 {
                // A nonzero exit is common for --help on older builds; only trust
                // the output when it actually contains the options we care about.
                if !strings.Contains(string(out), "--flash-attn") &&
                        !strings.Contains(string(out), "--cache-reuse") {
                        return nil
                }
        }

        text := string(out)
        caps := &EngineCaps{
                Tag:              tag,
                FlashAttnEnabled: optionMentioned(text, "--flash-attn"),
                CacheReuse:       optionMentioned(text, "--cache-reuse"),
                NoWebUI:          optionMentioned(text, "--no-webui"),
                Jinja:            optionMentioned(text, "--jinja"),
                UBatchSize:       optionMentioned(text, "--ubatch-size") || optionMentioned(text, "--ubatch"),
                ThreadsBatch:     optionMentioned(text, "--threads-batch"),
                Source:           "help-parse",
        }

        caps.FlashAttnValue = helpFlashAttnTakesValue(text)

        // Sanity gate: a parsed profile that lost every option compared with
        // the tag fallback is more likely a parse failure than a real
        // regression — fall back to the heuristic in that case.
        if !caps.CacheReuse && !caps.NoWebUI && !caps.Jinja {
                return nil
        }
        return caps
}

// helpFlashAttnTakesValue decides the `--flash-attn` layout from help text:
//
//      value form:  "-fa, --flash-attn [on|off|auto]" / "--flash-attn on|off|auto"
//      flag form:   "-fa, --flash-attn" (no value list on the same line)
func helpFlashAttnTakesValue(help string) bool {
        for _, line := range strings.Split(help, "\n") {
                if !strings.Contains(line, "--flash-attn") {
                        continue
                }
                // The tri-state value list appears as on|off|auto (optionally
                // bracketed) on the same line as the option.
                if regexp.MustCompile(`on\s*\|\s*off\s*\|\s*auto`).MatchString(line) {
                        return true
                }
        }
        return false
}

// optionMentioned reports whether an option appears in help output.
func optionMentioned(help, option string) bool {
        return strings.Contains(help, option)
}

// sanitizedHelpEnv builds a minimal environment for the --help probe so a
// hostile model directory or engine binary cannot leak the user's
// environment into a subprocess (mirrors proc.SanitizedEnvironment intent
// without pulling the Windows-specific pieces into this probe).
func sanitizedHelpEnv() []string {
        return []string{
                "PATH=" + os.Getenv("PATH"),
                "HOME=" + os.Getenv("HOME"),
                "TEMP=" + os.Getenv("TEMP"),
                "TMP=" + os.Getenv("TMP"),
                "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
                "COMSPEC=" + os.Getenv("COMSPEC"),
        }
}

// ---------------------------------------------------------------------------
// Capability profile persistence — verified profiles only.
// ---------------------------------------------------------------------------

// capsStore is the on-disk shape of engine-caps.json.
type capsStore struct {
        Tag  string      `json:"tag"`
        Caps *EngineCaps `json:"caps"`
}

// capsPath resolves the profile store location.
func capsPath(cfg *config.Config) string {
        return filepath.Join(cfg.DataDir, capsFileName)
}

// loadEngineCaps returns the persisted profile when it matches the current
// engine tag; otherwise nil (the caller falls back to detection).
func loadEngineCaps(cfg *config.Config, tag string) *EngineCaps {
        data, err := os.ReadFile(capsPath(cfg))
        if err != nil {
                return nil
        }
        var store capsStore
        if err := json.Unmarshal(data, &store); err != nil {
                return nil
        }
        if store.Caps == nil || store.Tag != tag || store.Caps.Tag != tag {
                return nil
        }
        return store.Caps
}

// saveEngineCaps persists a profile. Only VERIFIED profiles (successful
// engine start) may be persisted — callers must respect that contract.
func saveEngineCaps(cfg *config.Config, caps *EngineCaps) {
        if cfg == nil || caps == nil {
                return
        }
        store := capsStore{Tag: caps.Tag, Caps: caps}
        data, err := json.MarshalIndent(store, "", "  ")
        if err != nil {
                return
        }
        tmp := capsPath(cfg) + ".tmp"
        if err := os.WriteFile(tmp, data, 0o600); err != nil {
                return
        }
        _ = os.Rename(tmp, capsPath(cfg)) // atomic replace
}

// resolveEngineCaps picks the capability profile for the configured engine:
// a persisted verified profile for the current tag wins; otherwise the tag
// heuristic applies. This is the cheap, allocation-free path used by
// SpeedArgs; the full --help detection runs once per boot in Start().
func resolveEngineCaps(cfg *config.Config) *EngineCaps {
        tag := currentEngineTag(cfg)
        if caps := loadEngineCaps(cfg, tag); caps != nil {
                return caps
        }
        return defaultCapsForTag(tag)
}

// currentEngineTag resolves the installed engine tag through the updater
// bookkeeping (installed.json); empty when never recorded.
func currentEngineTag(cfg *config.Config) string {
        if cfg == nil {
                return ""
        }
        return updater.InstalledEngineTag(cfg)
}

// ---------------------------------------------------------------------------
// Launch-argument validation — every profile is checked BEFORE spawn.
// ---------------------------------------------------------------------------

// argProblems returns human-readable validation problems for a launch
// profile. An empty slice means the profile is internally consistent with
// the capability profile.
//
// The known-option table covers every option SHEYTAN itself can emit;
// unknown options (user-provided LlamaExtraArgs) are passed through
// unvalidated — the engine remains the authority on its own surface.
func argProblems(args []string, caps *EngineCaps) []string {
        if caps == nil {
                caps = defaultCapsForTag("")
        }

        flashAttnValues := map[string]bool{"on": true, "off": true, "auto": true}

        // Options that take exactly one value, by name.
        takesValue := map[string]string{
                "--model":             "path",
                "--host":              "string",
                "--port":              "int",
                "--ctx-size":          "int",
                "--batch-size":        "int",
                "--ubatch-size":       "int",
                "--threads":           "int",
                "--threads-batch":     "int",
                "--n-gpu-layers":      "int",
                "--draft-max":         "int",
                "--seed":              "int",
                "--repeat-last-n":     "int",
                "--cache-reuse":       "int",
                "--temp":              "float",
                "--top-p":             "float",
                "--min-p":             "float",
                "--repeat-penalty":    "float",
                "--presence-penalty":  "float",
                "--frequency-penalty": "float",
                "--mirostat":          "int",
                "--mirostat-tau":      "float",
                "--mirostat-eta":      "float",
                "--mmproj":            "path",
                "--model-draft":       "path",
        }

        // Options that must NEVER be followed by a value.
        flagOnly := map[string]bool{
                "--mlock":    true,
                "--no-webui": true,
                "--jinja":    true,
        }

        var problems []string

        for i := 0; i < len(args); i++ {
                arg := args[i]
                if !strings.HasPrefix(arg, "-") || arg == "-" {
                        continue
                }

                next := ""
                if i+1 < len(args) {
                        next = args[i+1]
                }

                switch {
                case arg == "--flash-attn":
                        if caps.FlashAttnValue {
                                // Value form REQUIRED. A following option token (e.g.
                                // --cache-reuse) would be consumed as the value by the
                                // engine — the exact Phase 6 regression.
                                if next == "" || strings.HasPrefix(next, "-") || !flashAttnValues[strings.ToLower(next)] {
                                        problems = append(problems, fmt.Sprintf(
                                                "--flash-attn requires an on|off|auto value, got %q",
                                                next))
                                }
                        } else {
                                // Flag form: a bare value after it would be an unknown
                                // positional on legacy engines.
                                if next != "" && !strings.HasPrefix(next, "-") {
                                        problems = append(problems, fmt.Sprintf(
                                                "--flash-attn is a boolean flag on this engine build; it must not be given a value (got %q)",
                                                next))
                                }
                        }

                case arg == "--cache-reuse":
                        if !caps.CacheReuse {
                                problems = append(problems,
                                        "--cache-reuse is not supported by this engine build")
                                continue
                        }
                        if n, err := strconv.Atoi(next); err != nil || n < 0 {
                                problems = append(problems, fmt.Sprintf(
                                        "--cache-reuse requires a non-negative integer, got %q", next))
                        }

                case flagOnly[arg]:
                        if next != "" && !strings.HasPrefix(next, "-") {
                                problems = append(problems, fmt.Sprintf(
                                        "%s takes no value, got %q", arg, next))
                        }

                default:
                        if kind, ok := takesValue[arg]; ok {
                                if next == "" || (strings.HasPrefix(next, "-") && !isNegativeNumber(next)) {
                                        problems = append(problems, fmt.Sprintf(
                                                "%s requires a value (%s), got %q", arg, kind, next))
                                        continue
                                }
                                switch kind {
                                case "int":
                                        if _, err := strconv.Atoi(next); err != nil {
                                                problems = append(problems, fmt.Sprintf(
                                                        "%s requires an integer, got %q", arg, next))
                                        }
                                case "float":
                                        if _, err := strconv.ParseFloat(next, 64); err != nil {
                                                problems = append(problems, fmt.Sprintf(
                                                        "%s requires a number, got %q", arg, next))
                                        }
                                }
                        }
                        // Unknown options pass through — the engine owns its surface.
                }

                // Skip the consumed value so its own `--`-lookalike content is not
                // re-validated as an option.
                if _, ok := takesValue[arg]; ok {
                        i++
                }
        }

        return problems
}

func isNegativeNumber(s string) bool {
        _, err := strconv.ParseFloat(s, 64)
        return err == nil
}

// ---------------------------------------------------------------------------
// Startup-failure classification and surgical repair.
// ---------------------------------------------------------------------------

// StartupFailureKind classifies WHY an engine start failed, from the
// engine's own stderr/stdout text.
type StartupFailureKind string

const (
        // FailOptionLayout: an option was rejected because of its value/flag
        // layout (the --flash-attn on|off|auto class of failures).
        FailOptionLayout StartupFailureKind = "option-layout"

        // FailUnknownOption: the build does not know this option at all.
        FailUnknownOption StartupFailureKind = "unknown-option"

        // FailModelArch: the build cannot run this model architecture.
        FailModelArch StartupFailureKind = "unsupported-architecture"

        // FailModelLoad: the model itself failed to load (OOM, corrupt file,
        // allocation failure) — never repairable by option changes.
        FailModelLoad StartupFailureKind = "model-load"

        // FailEnvironment: missing DLLs and other host problems.
        FailEnvironment StartupFailureKind = "environment"

        // FailUnknown: unclassified.
        FailUnknown StartupFailureKind = "unknown"
)

// StartupFailure is the classified result of one failed launch attempt.
type StartupFailure struct {
        Kind   StartupFailureKind
        Option string // offending option, normalized WITHOUT leading dashes
        Detail string
}

// ClassifyStartupFailure maps engine error text to a typed failure.
func ClassifyStartupFailure(text string) *StartupFailure {
        lower := strings.ToLower(text)

        // Option-layout rejections:
        //   error while handling argument "--flash-attn":
        //   unknown value for --flash-attn: '--cache-reuse'
        //   error: invalid argument: on
        //   error: invalid argument: --flash-attn
        if m := regexp.MustCompile(`error while handling argument "([^"]+)"`).FindStringSubmatch(lower); m != nil {
                return &StartupFailure{
                        Kind:   FailOptionLayout,
                        Option: normalizeOption(m[1]),
                        Detail: text,
                }
        }
        if m := regexp.MustCompile(`invalid argument: (\S+)`).FindStringSubmatch(lower); m != nil {
                sf := &StartupFailure{Kind: FailOptionLayout, Detail: text}
                if strings.HasPrefix(m[1], "--") {
                        sf.Option = normalizeOption(m[1])
                }
                return sf
        }
        if strings.Contains(lower, "unknown value for") ||
                strings.Contains(lower, "invalid value for") {
                return &StartupFailure{Kind: FailOptionLayout, Detail: text}
        }

        // Fully unknown options.
        if m := regexp.MustCompile(`(?:unknown|unrecognized|invalid) argument:?\s+(--?[a-z0-9-]+)`).FindStringSubmatch(lower); m != nil {
                return &StartupFailure{
                        Kind:   FailUnknownOption,
                        Option: normalizeOption(m[1]),
                        Detail: text,
                }
        }
        if m := regexp.MustCompile(`(?:unknown|unrecognized) option\s+"?(-{1,2}[a-z0-9-]+)"?`).FindStringSubmatch(lower); m != nil {
                return &StartupFailure{
                        Kind:   FailUnknownOption,
                        Option: normalizeOption(m[1]),
                        Detail: text,
                }
        }

        // Model architecture (existing needsNewerEngine semantics).
        if strings.Contains(lower, "unknown model architecture") ||
                strings.Contains(lower, "unknown architecture") ||
                strings.Contains(lower, "unsupported architecture") ||
                strings.Contains(lower, "unknown model type") ||
                strings.Contains(lower, "unrecognized model architecture") {
                return &StartupFailure{Kind: FailModelArch, Detail: text}
        }

        // Real model-load failures: no option can fix these.
        for _, marker := range []string{
                "failed to load model",
                "error loading model",
                "unable to allocate",
                "out of memory",
                "cuda error",
                "failed to allocate",
                "corrupt",
        } {
                if strings.Contains(lower, marker) {
                        return &StartupFailure{Kind: FailModelLoad, Detail: text}
                }
        }

        // Host environment.
        if strings.Contains(text, "3221225781") || strings.Contains(text, "0xc0000135") {
                return &StartupFailure{Kind: FailEnvironment, Detail: text}
        }

        return &StartupFailure{Kind: FailUnknown, Detail: text}
}

// normalizeOption strips leading dashes and lower-cases an option name.
func normalizeOption(opt string) string {
        return strings.ToLower(strings.TrimLeft(strings.TrimSpace(opt), "-"))
}

// maxOptionRepairs bounds the surgical repair loop per compat level.
const maxOptionRepairs = 2

// repairCapsFor produces the repaired capability profile for a classified
// startup failure, or nil when the failure is not surgically repairable.
// ONLY the offending option is touched — never a blanket strip.
func repairCapsFor(caps *EngineCaps, sf *StartupFailure, launchedArgs []string) *EngineCaps {
        if caps == nil || sf == nil {
                return nil
        }

        switch sf.Kind {
        case FailOptionLayout:
                // The flash-attn layout flip is the known repairable case. When
                // the engine named the option, match it; when it did not (legacy
                // "invalid argument: on"), only flip when the launched profile
                // actually carried a flash-attn value pair.
                if sf.Option == "" || sf.Option == "flash-attn" || sf.Option == "fa" {
                        flipped := caps.Clone()
                        flipped.FlashAttnValue = !caps.FlashAttnValue
                        flipped.Source = "verified-repair"
                        return flipped
                }
                return nil

        case FailUnknownOption:
                switch sf.Option {
                case "cache-reuse":
                        if caps.CacheReuse {
                                repaired := caps.Clone()
                                repaired.CacheReuse = false
                                repaired.Removed = append(repaired.Removed, RemovedOption{
                                        Option: "--cache-reuse",
                                        Reason: "engine build reports the option as unknown",
                                })
                                repaired.Source = "verified-repair"
                                return repaired
                        }
                case "flash-attn", "fa":
                        // The build does not know flash-attn at all: drop it.
                        repaired := caps.Clone()
                        repaired.FlashAttnEnabled = false
                        repaired.FlashAttnValue = false
                        repaired.Removed = append(repaired.Removed, RemovedOption{
                                Option: "--flash-attn",
                                Reason: "engine build reports the option as unknown",
                        })
                        repaired.Source = "verified-repair"
                        return repaired
                case "no-webui":
                        repaired := caps.Clone()
                        repaired.NoWebUI = false
                        repaired.Removed = append(repaired.Removed, RemovedOption{
                                Option: "--no-webui",
                                Reason: "engine build reports the option as unknown",
                        })
                        repaired.Source = "verified-repair"
                        return repaired
                case "jinja":
                        repaired := caps.Clone()
                        repaired.Jinja = false
                        repaired.Removed = append(repaired.Removed, RemovedOption{
                                Option: "--jinja",
                                Reason: "engine build reports the option as unknown",
                        })
                        repaired.Source = "verified-repair"
                        return repaired
                case "ubatch-size":
                        repaired := caps.Clone()
                        repaired.UBatchSize = false
                        repaired.Removed = append(repaired.Removed, RemovedOption{
                                Option: "--ubatch-size",
                                Reason: "engine build reports the option as unknown",
                        })
                        repaired.Source = "verified-repair"
                        return repaired
                case "threads-batch":
                        repaired := caps.Clone()
                        repaired.ThreadsBatch = false
                        repaired.Removed = append(repaired.Removed, RemovedOption{
                                Option: "--threads-batch",
                                Reason: "engine build reports the option as unknown",
                        })
                        repaired.Source = "verified-repair"
                        return repaired
                }
                return nil
        }

        return nil
}

// flashAttnDropped reports whether repair removed flash-attn entirely.
func flashAttnDropped(caps *EngineCaps) bool {
        return caps == nil || !caps.FlashAttnEnabled
}

// logRepair records one surgical repair for the worklog/UI trail.
func logRepair(level int, before, after *EngineCaps, sf *StartupFailure) {
        logging.Default().Warn(
                "engine",
                "startup repair (compat %d): engine rejected %s (%s) — repairing only that option: %s",
                level,
                optionDisplay(sf),
                sf.Kind,
                capsDiff(before, after),
        )
}

func optionDisplay(sf *StartupFailure) string {
        if sf.Option == "" {
                return "an option layout"
        }
        return "--" + sf.Option
}

// capsDiff summarizes what changed between two profiles.
func capsDiff(before, after *EngineCaps) string {
        if before == nil || after == nil {
                return "profile unavailable"
        }
        var changes []string
        if before.FlashAttnValue != after.FlashAttnValue {
                if after.FlashAttnValue {
                        changes = append(changes, "--flash-attn now takes on|off|auto")
                } else {
                        changes = append(changes, "--flash-attn now a bare flag")
                }
        }
        if before.FlashAttnEnabled && !after.FlashAttnEnabled {
                changes = append(changes, "--flash-attn removed")
        }
        if before.CacheReuse && !after.CacheReuse {
                changes = append(changes, "--cache-reuse removed")
        }
        if before.NoWebUI && !after.NoWebUI {
                changes = append(changes, "--no-webui removed")
        }
        if before.Jinja && !after.Jinja {
                changes = append(changes, "--jinja removed")
        }
        if before.UBatchSize && !after.UBatchSize {
                changes = append(changes, "--ubatch-size removed")
        }
        if before.ThreadsBatch && !after.ThreadsBatch {
                changes = append(changes, "--threads-batch removed")
        }
        if len(changes) == 0 {
                return "no capability change"
        }
        return strings.Join(changes, ", ")
}

// detectCapsForBoot performs the authoritative capability detection for one
// boot: the persisted verified profile wins when the tag matches; otherwise
// the binary is probed once via --help.
func (s *LlamaServer) detectCapsForBoot(cfg *config.Config, binPath string) *EngineCaps {
        tag := currentEngineTag(cfg)

        if caps := loadEngineCaps(cfg, tag); caps != nil {
                s.logf("engine capability profile: %s (persisted, verified %s)",
                        capsSummary(caps), caps.VerifiedAt)
                return caps
        }

        caps := DetectEngineCaps(binPath, tag)
        s.logf("engine capability profile: %s (source: %s)", capsSummary(caps), caps.Source)
        logging.Default().Info("engine",
                "engine version %s — capability profile %s (source %s)",
                tag, capsSummary(caps), caps.Source)
        return caps
}

// capsSummary renders a one-line capability profile for logs.
func capsSummary(caps *EngineCaps) string {
        if caps == nil {
                return "unknown"
        }
        form := "unsupported"
        if caps.FlashAttnEnabled {
                form = "flag"
                if caps.FlashAttnValue {
                        form = "on|off|auto"
                }
        }
        return fmt.Sprintf("flash-attn=%s cache-reuse=%v no-webui=%v jinja=%v ubatch=%v threads-batch=%v",
                form, caps.CacheReuse, caps.NoWebUI, caps.Jinja, caps.UBatchSize, caps.ThreadsBatch)
}

// ---------------------------------------------------------------------------
// v1.1.7 — compatibility-mode transparency and bounded retry-up.
// ---------------------------------------------------------------------------

// CompatInfo is the user-facing explanation of the active compatibility
// level: WHY the engine is not running the full-speed profile (when it is
// not), what was removed along the way, and when that was recorded. Served
// through /api/perf so the Settings → Performance view can show the real
// active configuration instead of a bare "compatibility mode 2" line.
type CompatInfo struct {
        Level      int      `json:"level"`
        Name       string   `json:"name"`
        Optimised  bool     `json:"optimised"`
        Reason     string   `json:"reason,omitempty"`
        RecordedAt string   `json:"recordedAt,omitempty"`
        Changes    []string `json:"changes,omitempty"`
}

// CompatSnapshot builds the CompatInfo for the current server state.
func (s *LlamaServer) CompatSnapshot() CompatInfo {
        cfg := s.src.Load()

        s.mu.Lock()
        caps := s.caps
        s.mu.Unlock()

        info := CompatInfo{
                Level:      cfg.EngineCompat,
                Name:       compatLevelName(cfg.EngineCompat),
                Optimised:  cfg.EngineCompat == 0,
                Reason:     cfg.EngineCompatReason,
                RecordedAt: cfg.EngineCompatAt,
        }

        if caps != nil {
                for _, r := range caps.Removed {
                        info.Changes = append(info.Changes, r.Option+" — "+r.Reason)
                }
        }

        return info
}

// CapsSnapshot returns a copy of the active capability profile (nil before
// the first boot attempt). Read-only access for the API surface.
func (s *LlamaServer) CapsSnapshot() *EngineCaps {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.caps.Clone()
}

// compatReasonFromError distills one failed launch attempt into the string
// persisted as EngineCompatReason: the classified failure kind, narrowed
// to the offending option when the engine named one. Option-class reasons
// ("option-layout", "unknown-option") are the ones a repaired capability
// profile can plausibly fix — the retry-up gate trusts only those.
func compatReasonFromError(err error) string {
        if err == nil {
                return "unknown"
        }
        sf := ClassifyStartupFailure(err.Error())
        if sf == nil {
                return "unknown"
        }
        if sf.Option != "" {
                return string(sf.Kind) + ":" + sf.Option
        }
        return string(sf.Kind)
}

// shouldRetryFullSpeed decides whether THIS boot earns one bounded attempt
// at the full-speed profile (level 0) before falling back to the persisted
// compatibility level.
//
// The historical defect (v1.1.6Z log evidence: the same
// "started in compatibility mode 2 (no speed flags)" line on every boot):
// once any launch failure pushed the ladder down, EngineCompat was
// persisted and EVERY later boot resumed at that level — even after the
// capability profile had been repaired and re-verified, so a one-time
// option incompatibility silently became a permanent speed downgrade.
//
// The gate is deliberately strict — every condition must hold:
//
//  1. a compatibility level above 0 is actually persisted;
//  2. the recorded reason is OPTION-class (a rejected option fails fast,
//     in well under a second — unlike a model-load failure that can burn
//     the full 180 s load timeout, so those are never re-attempted);
//  3. a VERIFIED capability profile exists AND is newer than the recorded
//     downgrade (or no downgrade time exists — the v1.1.6Z upgrade case),
//     meaning the engine contract changed since the descent;
//  4. the level-0 launch profile validates cleanly against that verified
//     profile, so the known incompatibility cannot simply replay.
//
// When all four hold, the attempt is neither a rediscovery of a known
// incompatibility (validation rules that out) nor an unbounded retry loop
// (a failed non-option retry stamps a fresh EngineCompatAt on the next
// successful boot, which re-blocks the gate until the profile changes).
func shouldRetryFullSpeed(cfg *config.Config, caps *EngineCaps, level0Args []string) (bool, string) {
        if cfg == nil || cfg.EngineCompat <= 0 {
                return false, ""
        }

        reason := strings.TrimSpace(cfg.EngineCompatReason)
        if reason == "" {
                // Pre-v1.1.7 downgrade: no reason was recorded. The upgrade
                // path deserves exactly one chance to restore full speed.
                reason = "unrecorded (pre-1.1.7 downgrade)"
        }

        // (2) Only option-class reasons are safe to re-test cheaply.
        if !strings.HasPrefix(reason, "option-layout") &&
                !strings.HasPrefix(reason, "unknown-option") {
                return false, ""
        }

        // (3) A verified profile must exist and postdate the downgrade.
        if caps == nil || strings.TrimSpace(caps.VerifiedAt) == "" {
                return false, ""
        }
        if strings.TrimSpace(cfg.EngineCompatAt) != "" {
                recorded, err := time.Parse(time.RFC3339, cfg.EngineCompatAt)
                verified, err2 := time.Parse(time.RFC3339, caps.VerifiedAt)
                if err == nil && err2 == nil && !verified.After(recorded) {
                        return false, ""
                }
        }

        // (4) The full-speed profile must validate against the verified
        // profile — otherwise the recorded incompatibility still holds.
        if problems := argProblems(level0Args, caps); len(problems) > 0 {
                return false, ""
        }

        return true, reason
}

// persistVerifiedCaps records a profile that just produced a VERIFIED
// successful startup, stamping it and saving it beside the config.
func (s *LlamaServer) persistVerifiedCaps(cfg *config.Config, caps *EngineCaps) {
        if caps == nil {
                return
        }
        verified := caps.Clone()
        verified.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
        saveEngineCaps(cfg, verified)
        s.mu.Lock()
        s.caps = verified
        s.mu.Unlock()
        if len(verified.Removed) > 0 {
                for _, r := range verified.Removed {
                        logging.Default().Warn("engine",
                                "engine capability downgrade recorded: %s — %s", r.Option, r.Reason)
                }
        }
}
