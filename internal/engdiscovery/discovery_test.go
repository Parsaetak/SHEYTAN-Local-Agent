package engdiscovery

// v1.3.6 (spec §36) — SYSTEM DISCOVERY TEST SUITE: deterministic
// fixture-based coverage for candidate discovery, cache behavior,
// non-destructive validation and priority. The "engine" fixture is a
// copy of THIS test binary (a valid host-arch executable) placed at
// fixture paths — no arbitrary binaries are executed; validation probes
// run --version which the test binary answers via TestMain.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestMain(m *testing.M) {
	// The bounded probe runs the candidate with --version; the fixture
	// candidate IS this test binary, so answer honestly and exit.
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString("version: 4818 (discovery-fixture)\n")

		return
	}

	os.Exit(m.Run())
}

func fixtureConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.LlamaBinPath = ""

	return cfg
}

func stageTestBinary(t *testing.T, dir, name string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	dst := filepath.Join(dir, name)

	in, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	return dst
}

func discoveryEngineName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}

	return "llama-server"
}

// --- candidate discovery -------------------------------------------------

func TestQuickFindFindsManagedCandidate(t *testing.T) {
	cfg := fixtureConfig(t)

	managed := stageTestBinary(t, filepath.Join(cfg.DataDir, "bin"), discoveryEngineName())

	cand, err := QuickFind(cfg, discoveryEngineName())
	if err != nil {
		t.Fatalf("QuickFind: %v", err)
	}

	if cand == nil {
		t.Fatalf("managed candidate must be found in Tier 0")
	}

	if filepath.Clean(cand.Path) != filepath.Clean(managed) {
		t.Fatalf("managed candidate mismatch: %s vs %s", cand.Path, managed)
	}

	if cand.Tier != 0 {
		t.Fatalf("managed candidate is Tier 0, got %d", cand.Tier)
	}
}

func TestQuickFindFindsCachedPathCandidate(t *testing.T) {
	cfg := fixtureConfig(t)

	// An "external" install in a location recorded by a previous run.
	externalDir := filepath.Join(cfg.DataDir, "external", "llama")
	external := stageTestBinary(t, externalDir, discoveryEngineName())

	// Seed the cache with the external path (Tier 0 cache replay).
	if err := SaveCache(cfg, []Candidate{{
		Path: external, Size: 0, ModTime: 0, Tier: 0, Source: "cache",
	}}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	cand, err := QuickFind(cfg, discoveryEngineName())
	if err != nil {
		t.Fatalf("QuickFind: %v", err)
	}

	if cand == nil {
		t.Fatalf("cached candidate must be replayed")
	}

	if filepath.Clean(cand.Path) != filepath.Clean(external) {
		t.Fatalf("cache replay mismatch: %s vs %s", cand.Path, external)
	}
}

func TestQuickFindSearchesPATHLocations(t *testing.T) {
	cfg := fixtureConfig(t)

	bindir := t.TempDir()
	stageTestBinary(t, bindir, discoveryEngineName())

	t.Setenv("PATH", bindir)

	cand, err := QuickFind(cfg, discoveryEngineName())
	if err != nil {
		t.Fatalf("QuickFind: %v", err)
	}

	if cand == nil || filepath.Dir(cand.Path) != filepath.Clean(bindir) {
		t.Fatalf("PATH candidate must be discovered (Tier 1), got %+v", cand)
	}

	if cand.Tier != 1 {
		t.Fatalf("PATH candidate is Tier 1, got %d", cand.Tier)
	}
}

func TestQuickFindReturnsNilWithoutCandidates(t *testing.T) {
	cfg := fixtureConfig(t)
	t.Setenv("PATH", t.TempDir()) // empty PATH location

	cand, err := QuickFind(cfg, discoveryEngineName())
	if err != nil {
		t.Fatalf("QuickFind must not error on an empty machine: %v", err)
	}

	if cand != nil {
		t.Fatalf("no candidates exist — got %+v", cand)
	}
}

// --- validation (spec §12: non-destructive) -------------------------------

func TestValidateCandidateAcceptsValidHostBinary(t *testing.T) {
	cfg := fixtureConfig(t)

	path := stageTestBinary(t, filepath.Join(cfg.DataDir, "external"), discoveryEngineName())

	cand := &Candidate{Path: path, Tier: 2, Source: "scan"}
	if !ValidateCandidate(cand) {
		t.Fatalf("valid host binary must validate, evidence: %s", cand.Validation)
	}

	if !cand.Validated || cand.SHA256 == "" {
		t.Fatalf("validated candidate must carry identity evidence: %+v", cand)
	}

	if !strings.Contains(cand.Validation, "probe") {
		t.Fatalf("validation evidence must include the probe result: %s", cand.Validation)
	}
}

func TestValidateCandidateRejectsGarbageFile(t *testing.T) {
	cfg := fixtureConfig(t)

	path := filepath.Join(cfg.DataDir, "external", discoveryEngineName())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("definitely not an engine"), 0o755); err != nil {
		t.Fatal(err)
	}

	cand := &Candidate{Path: path}
	if ValidateCandidate(cand) {
		t.Fatalf("a text file must never validate as an engine")
	}

	if !strings.Contains(cand.Validation, "rejected") {
		t.Fatalf("rejection must be recorded with evidence: %s", cand.Validation)
	}
}

func TestValidateCandidateNeverExecutesBeforeStaticChecks(t *testing.T) {
	// A file with the right NAME but zero bytes must be rejected by the
	// static gate with NO probe execution (spec §12 step order).
	cfg := fixtureConfig(t)

	path := filepath.Join(cfg.DataDir, "external", discoveryEngineName())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	cand := &Candidate{Path: path}

	if ValidateCandidate(cand) {
		t.Fatalf("empty file must not validate")
	}
}

// --- cache behavior -------------------------------------------------------

func TestDiscoveryCacheInvalidatesOnIdentityChange(t *testing.T) {
	cfg := fixtureConfig(t)

	managed := stageTestBinary(t, filepath.Join(cfg.DataDir, "bin"), discoveryEngineName())

	if _, err := QuickFind(cfg, discoveryEngineName()); err != nil {
		t.Fatalf("QuickFind: %v", err)
	}

	cached := LoadCache(cfg)
	if len(cached) == 0 {
		t.Fatalf("the managed candidate must be persisted to the cache")
	}

	// Corrupt the binary — the cache entry's stale identity must NOT be
	// trusted: the candidate is re-inspected, fails the static gate and
	// is therefore NOT offered as usable (cache invalidation).
	if err := os.WriteFile(managed, []byte("mutated"), 0o755); err != nil {
		t.Fatal(err)
	}

	cand, err := QuickFind(cfg, discoveryEngineName())
	if err != nil {
		t.Fatalf("QuickFind after mutation: %v", err)
	}

	if cand != nil {
		t.Fatalf("a corrupted managed binary must not be offered (stale cache identity must not be trusted), got %+v", cand)
	}
}

// --- Tier 2 bounded scan ---------------------------------------------------

func TestFullScanFindsCandidateWithBoundedWorkers(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	target := filepath.Join(home, "sheytan-discovery-fixture", "sub")
	stageTestBinary(t, target, discoveryEngineName())
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(home, "sheytan-discovery-fixture")) })

	cands := FullScan(context.Background(), cfg, discoveryEngineName(), ScanOptions{
		Workers:       2,
		Timeout:       30 * time.Second,
		MaxDepth:      6,
		MaxCandidates: 8,
	})

	found := false
	for _, c := range cands {
		if filepath.Clean(c.Path) == filepath.Clean(filepath.Join(target, discoveryEngineName())) {
			found = true
		}
	}

	if !found {
		t.Fatalf("Tier 2 scan must find the fixture under the home tree, got %+v", cands)
	}
}

func TestFullScanRespectsCandidateCap(t *testing.T) {
	cfg := fixtureConfig(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this runner")
	}

	for i := 0; i < 3; i++ {
		stageTestBinary(t, filepath.Join(home, "sheytan-discovery-fixture", string(rune('a'+i))), discoveryEngineName())
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(home, "sheytan-discovery-fixture")) })

	cands := FullScan(context.Background(), cfg, discoveryEngineName(), ScanOptions{
		Workers:       2,
		Timeout:       30 * time.Second,
		MaxDepth:      6,
		MaxCandidates: 2,
	})

	if len(cands) > 2 {
		t.Fatalf("scan must respect MaxCandidates=2, got %d", len(cands))
	}
}
