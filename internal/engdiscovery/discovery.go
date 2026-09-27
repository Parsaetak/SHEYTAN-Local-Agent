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
	"container/heap"
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
// Tier 2 — bounded priority-frontier scan (background only, spec §11)
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
// worker pool fed by a priority frontier (spec §11 Tier 2). Inaccessible
// directories are ignored without crashing; file contents are never read
// except for validation of promising candidates. The returned candidates
// are static metadata only — run Validate before importing anything.
//
// v1.7.3 — the traversal is a BOUNDED PRIORITY FRONTIER, not a recursive
// DFS. The previous per-root walkBound was lexical-order DFS: it descended
// one subtree to full depth before touching its next sibling, so a huge
// sibling directory (AppData on the Windows CI runners) could consume the
// entire Tier 2 time budget before a shallow, high-value location such as
// <home>\sheytan-discovery-fixture was ever read (Actions run
// 36311052375 / Windows job 108597318493). Root reordering (v1.4.0)
// cannot fix that class of defect — the starvation happens INSIDE the
// first root's own subtree. The frontier inverts the shape:
//
//	priority directory frontier → bounded worker pool → read directory
//	→ inspect matching engine file → enqueue child directories
//	   with (class, depth, name) priority
//
// Directories are visited (class, depth)-ordered — the user home first,
// per-user application roots and previously observed candidate parents
// second, broad volume recursion last, and every shallower level is
// exhausted before any deeper level is touched. A valid engine a few
// levels below home can no longer be blocked behind a giant unrelated
// subtree: each directory visit costs one ReadDir, and the fixture's
// ancestors are therefore reached within a handful of directory reads
// regardless of sibling size.
//
// Preserved contract (unchanged from v1.4.0): Tier 0/Tier 1 are separate
// synchronous passes (QuickFind); Tier 2 remains background-only; the
// hard total timeout, bounded workers, MaxDepth, MaxCandidates,
// cancellation, inaccessible-directory tolerance, noise-directory
// skipping, candidate deduplication, static inspection, architecture
// validation, bounded executable validation, cache integration and
// symlink/reparse-point safety all keep their previous semantics.
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

	f := newScanFrontier()

	// Seed the frontier in mission priority order: (1) the user's own
	// tree, (2) per-user application roots, (5) previously observed
	// candidate parents from the discovery cache; the broad volume
	// recursion (6) carries the lowest class. Seeding is ordered —
	// roots first, so an overlapping cache parent can never re-mark
	// the higher-priority home seed.
	for _, root := range roots {
		f.seed(root, scanRootClass(root))
	}
	for _, cached := range LoadCache(cfg) {
		f.seed(filepath.Dir(cached.Path), 1)
	}

	// Candidate sink: dedupe by path, cap at MaxCandidates, seal the
	// frontier when the cap is reached. The frontier's own mutex guards
	// the found slice — one lock domain for all scan state (v1.5.0 race
	// audit: every shared read/write stays under this mutex).
	var found []Candidate

	emit := func(cand Candidate) bool {
		f.mu.Lock()
		defer f.mu.Unlock()

		if f.stopped {
			return false
		}

		// Dedupe by path.
		for _, prev := range found {
			if filepath.Clean(prev.Path) == filepath.Clean(cand.Path) {
				return true
			}
		}

		found = append(found, cand)

		if len(found) >= opts.MaxCandidates {
			f.stopped = true
			f.cond.Broadcast()
			return false
		}

		return true
	}

	// Bounded worker pool: at most opts.Workers goroutines ever exist,
	// each pulling the next-highest-priority directory from the frontier.
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				if scanCtx.Err() != nil {
					return
				}

				job, ok := f.next()
				if !ok {
					return
				}

				f.visit(scanCtx, job, name, opts, emit)
				f.release()
			}
		}()
	}

	// Cancellation watcher: wake every worker blocked on the frontier the
	// moment the budget expires (hard total timeout, spec §11). Without
	// this a worker waiting on an idle-but-active frontier would only
	// notice cancellation when another worker happened to broadcast. No
	// leak: the watcher exits as soon as the workers finish.
	finished := make(chan struct{})

	go func() {
		select {
		case <-scanCtx.Done():
			f.stop()
		case <-finished:
		}
	}()

	wg.Wait()
	close(finished)

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

// scanRootClass ranks a scan root for the priority frontier (lower is
// visited earlier): (0) the user's own tree, (1) per-user application
// roots, (2) broad volume recursion — the discovery priority order of
// spec §11. Overlapping roots are collapsed later by the frontier's
// visited set (e.g. LOCALAPPDATA lives inside the home tree), so the
// same directory is never scanned twice.
func scanRootClass(root string) int {
	clean := filepath.Clean(root)

	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return 0
	}

	if local, ok := localAppData(); ok && clean == filepath.Clean(local) {
		return 1
	}

	return 2
}

// scanRoots enumerates the volumes/roots to scan. On Windows that is the
// available drive letters; on Unix a small curated root set (bounded —
// never a blind / walk).
//
// v1.4.0 (run 35996462352): the USER TREE is scanned FIRST on every
// platform. On Windows the previous drive-letter-only root set meant a
// user-installed engine sat behind a full C:\ walk — deep behind
// AppData/Program Files alphabetically — so the bounded Tier 2 budget
// expired before the highest-value locations were reached, and
// discovery effectively never found user installs on Windows. Putting
// the home directory (and LOCALAPPDATA) at the FRONT of the root list
// matches the Unix behavior, reaches the most likely install locations
// within milliseconds, and lets the slower full-volume walks continue
// in parallel behind them.
func scanRoots() []string {
	var roots []string

	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		clean := filepath.Clean(dir)
		if seen[clean] {
			return
		}
		seen[clean] = true
		roots = append(roots, clean)
	}

	// Highest-value roots first: the user's own tree (and, on Windows,
	// the per-user AppData location where engines are commonly
	// installed).
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}

	if runtime.GOOS == "windows" {
		if local, ok := localAppData(); ok {
			add(local)
		}

		for c := 'C'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				add(root)
			}
		}

		return roots
	}

	// Unix: user trees and common install prefixes only.
	add("/usr/local")
	add("/opt")
	add("/snap")

	return roots
}

// ---------------------------------------------------------------------------
// Priority directory frontier (the Tier 2 traversal engine)
// ---------------------------------------------------------------------------

// Frontier priority arithmetic. A job's priority is
//
//	class*prioClassBand + depth*prioDepthBand - nameBonus
//
// so root class dominates, then depth (strictly: the name bonus is
// smaller than one depth band, so a "high-value" directory can never
// overtake a shallower level), then enqueue order breaks exact ties.
// Depth is capped far below the class band so the arithmetic can never
// leak a deep directory into a higher class.
const (
	prioClassBand = 1_000_000_000
	prioDepthBand = 100_000
	prioNameBonus = 50_000
	prioDepthCap  = 9_999

	// maxQueuedDirs is a defensive memory bound on the frontier. The
	// scan budget (timeout/cancellation) is the real limiter — no real
	// scan visits remotely this many directories — but a pathological
	// directory with millions of subdirectories must not be able to
	// grow the heap without bound. Past the cap, breadth is dropped
	// (shallow levels enqueue first, so the highest-value work is
	// never the part dropped).
	maxQueuedDirs = 200_000
)

// priorityDirNames are directory names that historically host engine
// installs (spec §11 Tier 1 locations, plus the product's own roots).
// A child with one of these names is visited ahead of its same-depth
// siblings (mission priority: common bin/Programs/llama.cpp locations).
var priorityDirNames = map[string]bool{
	"bin": true, "programs": true, "llama.cpp": true, "scoop": true,
	"shims": true, ".local": true, "sheytan-la": true,
	"sheytan-local-agent": true,
}

// dirJob is one directory in the frontier.
type dirJob struct {
	path  string
	depth int   // depth below its seed root
	class int   // root class (0 = home, 1 = per-user roots, 2 = volumes)
	prio  int64 // priority; lower is visited earlier
	seq   int64 // enqueue order — deterministic tiebreak
}

// dirHeap orders dirJobs by (prio, seq).
type dirHeap []*dirJob

func (h dirHeap) Len() int { return len(h) }

func (h dirHeap) Less(i, j int) bool {
	if h[i].prio != h[j].prio {
		return h[i].prio < h[j].prio
	}

	return h[i].seq < h[j].seq
}

func (h dirHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *dirHeap) Push(x any) { *h = append(*h, x.(*dirJob)) }

func (h *dirHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]

	return it
}

// scanFrontier is the bounded priority frontier: a mutex-guarded
// container/heap plus a condition variable that implements the classic
// parallel-BFS termination protocol (a worker may only exit when the
// heap is empty AND no worker is processing a job that could enqueue
// more work). All state lives under one mutex; no production path ever
// sleeps — waiting is condition-variable based, so shutdown is
// immediate at budget expiry, at MaxCandidates, or at full drain.
type scanFrontier struct {
	mu      sync.Mutex
	cond    *sync.Cond
	jobs    dirHeap
	visited map[string]bool
	seq     int64
	active  int
	stopped bool
}

func newScanFrontier() *scanFrontier {
	f := &scanFrontier{visited: map[string]bool{}}
	f.cond = sync.NewCond(&f.mu)

	return f
}

// seed enqueues a scan root at depth 0.
func (f *scanFrontier) seed(path string, class int) {
	f.enqueue(path, 0, class)
}

// enqueue adds one directory unless the scan is stopped, the directory
// was already queued (the visited set also collapses overlapping roots,
// so a directory is never scanned twice), or the defensive queue cap is
// reached.
func (f *scanFrontier) enqueue(path string, depth int, class int) {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stopped || f.visited[clean] || len(f.jobs) >= maxQueuedDirs {
		return
	}

	f.visited[clean] = true

	if depth > prioDepthCap {
		depth = prioDepthCap
	}

	bonus := int64(0)
	if priorityDirNames[strings.ToLower(filepath.Base(clean))] {
		bonus = prioNameBonus
	}

	prio := int64(class)*prioClassBand + int64(depth)*prioDepthBand - bonus
	if prio < 0 {
		prio = 0
	}

	f.seq++
	heap.Push(&f.jobs, &dirJob{path: clean, depth: depth, class: class, prio: prio, seq: f.seq})
	f.cond.Broadcast()
}

// stop seals the frontier: no further enqueues are accepted and every
// worker drains out at its next check.
func (f *scanFrontier) stop() {
	f.mu.Lock()

	if !f.stopped {
		f.stopped = true
		f.cond.Broadcast()
	}

	f.mu.Unlock()
}

// next blocks for the highest-priority job. It returns ok=false when the
// scan is stopped or the frontier is fully drained (heap empty and no
// worker processing — the only safe exit, because a running worker may
// still enqueue children).
func (f *scanFrontier) next() (dirJob, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for {
		if f.stopped {
			return dirJob{}, false
		}

		if len(f.jobs) > 0 {
			job := heap.Pop(&f.jobs).(*dirJob)
			f.active++

			return *job, true
		}

		if f.active == 0 {
			return dirJob{}, false
		}

		f.cond.Wait()
	}
}

// release marks one job fully processed and wakes peers when the last
// one finishes so they can observe the drained frontier.
func (f *scanFrontier) release() {
	f.mu.Lock()
	f.active--

	if f.active == 0 {
		f.cond.Broadcast()
	}

	f.mu.Unlock()
}

// isStopped reports whether the scan has been sealed.
func (f *scanFrontier) isStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.stopped
}

// visit processes one directory: read it, inspect any matching engine
// file, enqueue its child directories with their own priority. Errors
// (inaccessible directories, permission walls) are tolerated silently —
// spec §11 requires the scan to continue without crashing.
func (f *scanFrontier) visit(
	ctx context.Context,
	job dirJob,
	engineName string,
	opts ScanOptions,
	emit func(Candidate) bool,
) {
	entries, err := os.ReadDir(job.path)
	if err != nil {
		return // inaccessible — ignore without crashing (spec §11)
	}

	for _, e := range entries {
		if ctx.Err() != nil || f.isStopped() {
			return
		}

		name := e.Name()

		if e.IsDir() {
			// Symlink/reparse-point safety: ReadDir never reports a
			// symlink (or a Windows junction/reparse point) as a plain
			// directory, so only real directories are followed — a
			// link named like the engine falls through to the file
			// branch below, where inspectCandidate stats the TARGET
			// and still rejects directories. No walk can loop.
			if noiseDirs[strings.ToLower(name)] {
				continue
			}

			// Root-level dot-directories are skipped (preserved from
			// the v1.3.6 walk: saves the giant runner tool trees that
			// live directly under the home root without hiding deeper
			// user locations).
			if job.depth == 0 && strings.HasPrefix(name, ".") {
				continue
			}

			if job.depth+1 > opts.MaxDepth {
				continue
			}

			f.enqueue(filepath.Join(job.path, name), job.depth+1, job.class)

			continue
		}

		if !strings.EqualFold(name, engineName) {
			continue
		}

		if cand, ok := inspectCandidate(filepath.Join(job.path, name), 2, "scan"); ok {
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
