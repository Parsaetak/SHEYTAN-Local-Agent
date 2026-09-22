// Package engdiscovery implements System Engine Discovery (v1.3.6,
// spec §10–§13): before downloading llama.cpp, intelligently search the
// machine for an already-installed compatible llama.cpp / SHEYTAN engine
// package and reuse it after validation.
//
// Discovery is TIERED (spec §11):
//
//	Tier 0 — managed local state: the managed bin dir, the persisted
//	         discovery cache, previously known-good candidates.
//	Tier 1 — cheap system locations: PATH, exe dir, sibling bin dirs,
//	         common user/package locations.
//	Tier 2 — full system scan: bounded parallel walk, noise-skipping,
//	         time-bounded. NEVER runs on the startup path.
//
// Discovery is NON-DESTRUCTIVE (spec §12): no candidate is executed
// because of its filename. Static evidence first (architecture, PE
// import closure), a bounded probe only after static checks pass.
package engdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// Candidate is one discovered engine package entry point.
type Candidate struct {
	Path       string    `json:"path"`
	Kind       string    `json:"kind"` // "llama-server" | "shtn-engine-host"
	Size       int64     `json:"size"`
	ModTime    int64     `json:"modTimeUnix"`
	SHA256     string    `json:"sha256,omitempty"`
	Arch       string    `json:"arch,omitempty"`
	Format     string    `json:"format,omitempty"`
	Tag        string    `json:"tag,omitempty"`
	Tier       int       `json:"tier"`
	Source     string    `json:"source,omitempty"` // volume / location class
	Validated  bool      `json:"validated,omitempty"`
	Validation string    `json:"validation,omitempty"` // evidence string
	FoundAt    time.Time `json:"foundAt"`
}

// cacheFile is the persisted discovery cache (second startup is
// substantially cheaper — no re-scan, revalidation only).
const cacheFile = "discovery-cache.json"

func cachePath(cfg *config.Config) string {
	return filepath.Join(cfg.DataDir, cacheFile)
}

// LoadCache reads the persisted discovery cache.
func LoadCache(cfg *config.Config) []Candidate {
	data, err := os.ReadFile(cachePath(cfg))
	if err != nil {
		return nil
	}

	var cands []Candidate
	if err := json.Unmarshal(data, &cands); err != nil {
		return nil
	}

	return cands
}

// SaveCache persists the discovery cache atomically.
func SaveCache(cfg *config.Config, cands []Candidate) error {
	data, err := json.MarshalIndent(cands, "", "  ")
	if err != nil {
		return err
	}

	tmp := cachePath(cfg) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, cachePath(cfg))
}

// QuickFind runs the synchronous Tier 0 + Tier 1 pass. It is fast
// (bounded directory set, no deep walk) and safe on the boot path.
// Returns nil when no usable candidate is found — callers then fall
// back to downloading (never Tier 2 synchronously).
func QuickFind(cfg *config.Config, engineName string) (*Candidate, error) {
	name := engineName
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	var cands []Candidate

	// Tier 0a: managed bin dir (already the active location).
	managed := filepath.Join(updaterEngineBinDir(cfg), name)
	if cand, ok := inspectCandidate(managed, 0, "managed-bin"); ok {
		cands = append(cands, *cand)
	}

	// Tier 0b: persisted cache — validated candidates from a previous
	// run, revalidated by identity (size+mtime; hash only on mismatch).
	for _, cached := range LoadCache(cfg) {
		if filepath.Clean(cached.Path) == filepath.Clean(managed) {
			continue
		}

		if cand, ok := inspectCandidate(cached.Path, 0, "cache"); ok {
			if cand.Size == cached.Size && cand.ModTime == cached.ModTime {
				cand.SHA256 = cached.SHA256
				cand.Tag = cached.Tag
				cand.Validated = cached.Validated
				cand.Validation = cached.Validation
			}

			cands = append(cands, *cand)
		}
	}

	// Tier 1: cheap system locations.
	for _, dir := range tier1Directories(cfg) {
		p := filepath.Join(dir, name)
		if cand, ok := inspectCandidate(p, 1, "tier1:"+dir); ok {
			cands = append(cands, *cand)
		}
	}

	best := pickBest(cands, managed)
	if best == nil {
		return nil, nil
	}

	// The managed binary itself is always acceptable (preflight
	// validates it downstream); an external candidate must at least
	// match the host architecture before it is interesting.
	if filepath.Clean(best.Path) != filepath.Clean(managed) {
		if !engcheck.ArchMatchesHost(best.Arch) {
			return nil, nil
		}
	}

	mergeIntoCache(cfg, *best)

	return best, nil
}

// updaterEngineBinDir avoids an import cycle with internal/updater by
// resolving the managed bin dir locally (the derivation is one line and
// pinned by tests in config + updater).
func updaterEngineBinDir(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return filepath.Dir(cfg.LlamaBinPath)
	}

	return filepath.Join(cfg.DataDir, "bin")
}

// tier1Directories lists the cheap search locations (spec §11 Tier 1):
// PATH, the executable directory, sibling bin directories and common
// user application / package locations.
func tier1Directories(cfg *config.Config) []string {
	var dirs []string

	seen := map[string]bool{}
	add := func(d string) {
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	// PATH entries.
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		add(p)
	}

	// Executable directory + sibling bin.
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		add(exeDir)
		add(filepath.Join(exeDir, "bin"))
		add(filepath.Join(filepath.Dir(exeDir), "bin"))
	}

	// The config's own data dir bin (managed) — harmless duplicate.
	add(filepath.Join(cfg.DataDir, "bin"))

	// Common user application locations.
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, "bin"))
		add(filepath.Join(home, "scoop", "shims"))
		add(filepath.Join(home, "AppData", "Local", "Programs"))
	}

	if local, ok := localAppData(); ok {
		add(filepath.Join(local, "Programs"))
		add(filepath.Join(local, "llama.cpp"))
		add(filepath.Join(local, "SHEYTAN-LA", "bin"))
	}

	// Previously observed candidate locations (cache dirs of past runs).
	for _, c := range LoadCache(cfg) {
		add(filepath.Dir(c.Path))
	}

	return dirs
}

func localAppData() (string, bool) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v, true
		}
	}

	if v, err := os.UserCacheDir(); err == nil {
		return v, true
	}

	return "", false
}

// inspectCandidate collects static metadata for one path. ok is false
// when the path is not a plausible engine binary (missing, directory,
// zero bytes, or unrecognizable format).
func inspectCandidate(path string, tier int, source string) (*Candidate, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() == 0 {
		return nil, false
	}

	arch, format := engcheck.SniffArch(path)
	if format == "" {
		return nil, false
	}

	return &Candidate{
		Path:    path,
		Size:    st.Size(),
		ModTime: st.ModTime().Unix(),
		Arch:    arch,
		Format:  format,
		Tier:    tier,
		Source:  source,
		FoundAt: time.Now().UTC(),
	}, true
}

// pickBest orders candidates: the managed binary first, then lowest
// tier, then largest binary (a real server build beats a stub).
func pickBest(cands []Candidate, managed string) *Candidate {
	if len(cands) == 0 {
		return nil
	}

	best := 0
	for i := range cands {
		a, b := cands[i], cands[best]

		aManaged := filepath.Clean(a.Path) == filepath.Clean(managed)
		bManaged := filepath.Clean(b.Path) == filepath.Clean(managed)

		switch {
		case aManaged && !bManaged:
			best = i
		case aManaged == bManaged && a.Tier < b.Tier:
			best = i
		case aManaged == bManaged && a.Tier == b.Tier && a.Size > b.Size:
			best = i
		}
	}

	c := cands[best]

	return &c
}

func mergeIntoCache(cfg *config.Config, cand Candidate) {
	cands := LoadCache(cfg)

	for i := range cands {
		if filepath.Clean(cands[i].Path) == filepath.Clean(cand.Path) {
			cands[i] = cand
			_ = SaveCache(cfg, cands)
			return
		}
	}

	cands = append(cands, cand)
	_ = SaveCache(cfg, cands)
}

// ---------------------------------------------------------------------------
// Tier 2 — bounded full-system scan (background only, spec §11)
// ---------------------------------------------------------------------------

// ScanOptions bounds a Tier 2 scan.
type ScanOptions struct {
	// Workers bounds the parallel directory walk (bounded worker pool).
	Workers int

	// Timeout bounds the whole scan.
	Timeout time.Duration

	// MaxDepth bounds recursion depth per volume.
	MaxDepth int

	// MaxCandidates stops the scan after this many plausible candidates.
	MaxCandidates int
}

// DefaultScanOptions is the conservative default: 4 workers, 90 s, deep
// enough for user trees, candidate-capped.
func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		Workers:       4,
		Timeout:       90 * time.Second,
		MaxDepth:      12,
		MaxCandidates: 16,
	}
}

// noiseDirs are skipped wholesale during Tier 2 scans: OS internals,
// package-manager noise and VCS dirs that can never contain a user's
// engine install (bounded scan, no wasted IO).
var noiseDirs = map[string]bool{
	"$recycle.bin": true, "windows": true, "winsxs": true, "system volume information": true,
	"node_modules": true, ".git": true, ".hg": true, ".svn": true,
	"proc": true, "sys": true, "dev": true, "run": true,
	"vendor": true, "cache": true, "temporary items": true, "vmware": true,
}

// FullScan enumerates available volumes and scans them with a bounded
// worker pool (spec §11 Tier 2). Inaccessible directories are ignored
// without crashing; file contents are never read except for validation
// of promising candidates. The returned candidates are static metadata
// only — run Validate before importing anything.
func FullScan(ctx context.Context, cfg *config.Config, engineName string, opts ScanOptions) []Candidate {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 90 * time.Second
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 12
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 16
	}

	name := engineName
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}

	roots := scanRoots()

	scanCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var (
		mu      sync.Mutex
		found   []Candidate
		wg      sync.WaitGroup
		sem     = make(chan struct{}, opts.Workers)
		stopped = false
	)

	for _, root := range roots {
		if stopped {
			break
		}

		wg.Add(1)

		go func(root string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			walkBound(scanCtx, root, name, opts, 0, func(cand Candidate) bool {
				mu.Lock()
				defer mu.Unlock()

				if stopped {
					return false
				}

				// Dedupe by path.
				for _, f := range found {
					if filepath.Clean(f.Path) == filepath.Clean(cand.Path) {
						return true
					}
				}

				found = append(found, cand)

				if len(found) >= opts.MaxCandidates {
					stopped = true
					return false
				}

				return true
			})
		}(root)
	}

	wg.Wait()

	// Validate the most promising candidates now (bounded: each is a
	// static check + one bounded probe).
	for i := range found {
		if scanCtx.Err() != nil {
			break
		}

		if engcheck.ArchMatchesHost(found[i].Arch) {
			ValidateCandidate(&found[i])
		}
	}

	mergeCacheAll(cfg, found)

	logging.Default().Info("engine",
		"system engine discovery (tier 2): scanned %d root(s), %d candidate(s)",
		len(roots), len(found))

	return found
}

// scanRoots enumerates the volumes/roots to scan. On Windows that is the
// available drive letters; on Unix a small curated root set (bounded —
// never a blind / walk).
func scanRoots() []string {
	var roots []string

	if runtime.GOOS == "windows" {
		for c := 'C'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				roots = append(roots, root)
			}
		}

		return roots
	}

	// Unix: user trees and common install prefixes only.
	extra := []string{"/usr/local", "/opt", "/snap"}

	if home, err := os.UserHomeDir(); err == nil {
		extra = append(extra, home)
	}

	roots = append(roots, extra...)

	return roots
}

// walkBound walks one root with a depth bound, skipping noise dirs,
// feeding matching files to emit. emit returning false stops the walk.
func walkBound(
	ctx context.Context,
	root, engineName string,
	opts ScanOptions,
	depth int,
	emit func(Candidate) bool,
) {
	if ctx.Err() != nil || depth > opts.MaxDepth {
		return
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return // inaccessible — ignore without crashing (spec §11)
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}

		name := e.Name()

		if e.IsDir() {
			if noiseDirs[strings.ToLower(name)] || strings.HasPrefix(name, ".") && depth == 0 {
				continue
			}

			walkBound(ctx, filepath.Join(root, name), engineName, opts, depth+1, emit)

			continue
		}

		if !strings.EqualFold(name, engineName) {
			continue
		}

		if cand, ok := inspectCandidate(filepath.Join(root, name), 2, "scan"); ok {
			if !emit(*cand) {
				return
			}
		}
	}
}

func mergeCacheAll(cfg *config.Config, cands []Candidate) {
	if len(cands) == 0 {
		return
	}

	existing := LoadCache(cfg)

	byPath := map[string]Candidate{}
	for _, c := range existing {
		byPath[filepath.Clean(c.Path)] = c
	}

	for _, c := range cands {
		byPath[filepath.Clean(c.Path)] = c
	}

	out := make([]Candidate, 0, len(byPath))
	for _, c := range byPath {
		out = append(out, c)
	}

	_ = SaveCache(cfg, out)
}

// ---------------------------------------------------------------------------
// Validation (spec §12) — static evidence first, bounded probe last
// ---------------------------------------------------------------------------

// ValidateCandidate runs the non-destructive validation ladder on one
// candidate and stamps the evidence into it:
//
//  1. static gate: exists / executable bit / recognizable format / arch;
//  2. dependency closure: PE imports resolved beside the binary;
//  3. bounded safe probe (--version) — ONLY after 1+2 pass.
//
// The probe output is recorded as evidence, never trusted as
// configuration.
func ValidateCandidate(cand *Candidate) bool {
	if cand == nil {
		return false
	}

	ident, err := engcheck.StaticValidate(cand.Path)
	if err != nil {
		cand.Validation = "rejected: " + err.Error()

		return false
	}

	cand.SHA256 = ident.SHA256
	cand.Arch = ident.Arch
	cand.Format = ident.Format

	deps, err := engcheck.CheckDependencies(cand.Path)
	if err == nil && len(deps.Missing) > 0 {
		cand.Validation = "rejected: missing runtime DLLs: " + strings.Join(deps.Missing, ", ")

		return false
	}

	// Bounded probe — the binary passed every static check; running its
	// --version is now evidence-gathering, not blind execution.
	ver, probeErr := probeVersion(cand.Path, 10*time.Second)
	if probeErr != nil {
		cand.Validation = "rejected: probe failed: " + probeErr.Error()

		return false
	}

	cand.Tag = probeTag(ver)
	cand.Validated = true
	cand.Validation = "validated: arch " + ident.Arch + ", probe: " + ver

	return true
}

// probeVersion runs --version with a hard timeout and returns the first
// output line. It reports (not swallows) failures.
func probeVersion(path string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := runBounded(ctx, path)
	if err != nil {
		return "", err
	}

	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			line = strings.TrimSpace(l)
			break
		}
	}

	if line == "" {
		return "", errors.New("empty --version output")
	}

	return line, nil
}

// probeTag extracts a build tag from a version line like
// "version: 4818 (abcd1234)" → "b4818".
func probeTag(versionLine string) string {
	v := strings.ToLower(versionLine)

	if i := strings.Index(v, "version:"); i >= 0 {
		fields := strings.Fields(v[i:])

		if len(fields) >= 2 {
			t := strings.TrimPrefix(fields[1], "b")

			if allDigits(t) {
				return "b" + t
			}
		}
	}

	return ""
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
