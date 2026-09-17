package llm

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestMain implements the fake llama.cpp engine: when the test binary is
// re-executed by LlamaServer itself (the engine-under-test spawns
// cfg.LlamaBinPath as a subprocess), the env marker turns this binary into
// a stand-in llama-server that serves /health on the requested port. This
// is the only portable way to test the REAL spawn → health → ready path
// without shipping an actual llama.cpp binary into CI.
func TestMain(m *testing.M) {
	if os.Getenv("SHEYTAN_FAKE_LLAMA") == "1" {
		runFakeLlamaServer()

		return
	}

	os.Exit(m.Run())
}

// runFakeLlamaServer serves /health (200) and optionally /v1/chat/completions
// until killed. GO_FAKE_LLAMA_MODE=crash makes it exit shortly after becoming
// healthy, driving the watchdog's bounded auto-restart.
func runFakeLlamaServer() {
	port := 0

	args := os.Args
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port, _ = strconv.Atoi(args[i+1])
		}
	}

	mode := os.Getenv("GO_FAKE_LLAMA_MODE")

	// Phase 7: record the argv the engine was launched with so tests can
	// assert the actual launch contract.
	if out := os.Getenv("SHEYTAN_FAKE_ARGS_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(strings.Join(args, "\n")), 0o644)
	}

	// Phase 7: strict CLI-contract emulation.
	//
	// strict-new-args    — mimics the on|off|auto contract: a --flash-attn
	//                      followed by anything else reproduces the EXACT
	//                      historical error and exits 1.
	// strict-legacy-args — mimics the boolean-flag contract: a
	//                      --flash-attn followed by a value is rejected.
	strictNew := mode == "strict-new-args"
	strictLegacy := mode == "strict-legacy-args"
	if strictNew || strictLegacy {
		for i, a := range args {
			if a != "--flash-attn" {
				continue
			}
			next := ""
			if i+1 < len(args) {
				next = args[i+1]
			}
			isValue := next == "on" || next == "off" || next == "auto"

			if strictNew {
				// New contract: a value is REQUIRED. The next option
				// being consumed as the value is the historical
				// Phase 6 failure.
				if next == "" || strings.HasPrefix(next, "--") || !isValue {
					fmt.Fprintf(os.Stderr,
						"error while handling argument \"--flash-attn\": \nunknown value for --flash-attn: '%s'\n",
						next)
					os.Exit(1)
				}
			}
			if strictLegacy && isValue {
				fmt.Fprintf(os.Stderr,
					"error: invalid argument: %s\n", next)
				os.Exit(1)
			}
		}
	}

	if port == 0 {
		os.Exit(2)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"fake-model.gguf"}]}`))
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":16384}}`))
	})

	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}

	if mode == "crash" {
		go func() {
			// Become healthy, then die — the watchdog must observe a real
			// process death while running.
			time.Sleep(500 * time.Millisecond)
			_ = server.Close()
			os.Exit(1)
		}()
	}

	_ = server.ListenAndServe()
	// Exit when the server closes or the parent kills us.
	os.Exit(0)
}

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}

	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func fakeEngineConfig(t *testing.T, mode string) (*config.Config, string) {
	t.Helper()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	dir := t.TempDir()

	// A fake but structurally valid model file: ResolveModelPath only
	// requires an existing .gguf file in the models dir.
	// v1.2.0: build the paths with the OS separator (filepath.Join), not
	// string concatenation — a forward-slash spelling on Windows made the
	// test's EXPECTED model path differ from the runtime's OS-canonical
	// "loaded model" report. See sameFilePath below.
	modelPath := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelPath, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}

	modelFile := filepath.Join(modelPath, "fake-model.gguf")
	if err := os.WriteFile(modelFile, []byte("fake gguf payload"), 0o644); err != nil {
		t.Fatalf("model file: %v", err)
	}

	cfg := config.Default()
	cfg.DataDir = dir
	cfg.ModelsDir = modelPath
	cfg.Provider = "local"
	cfg.LlamaBinPath = testBin
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = freePort(t)
	cfg.EngineCompat = 3 // bare flags: the fake engine ignores all tuning
	cfg.Model = "fake-model.gguf"
	cfg.VisionEnabled = false

	t.Setenv("SHEYTAN_FAKE_LLAMA", "1")

	if mode != "" {
		t.Setenv("GO_FAKE_LLAMA_MODE", mode)
	}

	return cfg, modelFile
}

func TestEngineStartReachesReady(t *testing.T) {
	cfg, modelFile := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if got := srv.State(); got != StateIdle {
		t.Fatalf("fresh engine must be idle, got %s", got)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := srv.State(); got != StateReady {
		t.Fatalf("healthy engine must report ready, got %s", got)
	}

	if !srv.IsRunning() || !srv.IsAlive() {
		t.Fatal("ready engine must count as alive/running")
	}

	// v1.2.0: OS-aware canonical comparison — never raw path strings.
	assertSameFilePath(t, srv.LoadedModel(), modelFile)

	if srv.Pid() <= 0 {
		t.Fatal("ready engine must expose a pid")
	}
}

func TestEngineStartFailsWithMissingBinary(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")
	cfg.LlamaBinPath = "/nonexistent/llama-server-missing"

	srv := NewLlamaServer(config.NewSource(cfg))

	err := srv.Start()
	if err == nil {
		t.Fatal("missing binary must fail")
	}

	if got := srv.State(); got != StateFailed {
		t.Fatalf("failed boot must report failed, got %s", got)
	}
}

func TestEngineStartFailsWithNoModel(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	dir := t.TempDir()
	cfg.ModelsDir = filepath.Join(dir, "empty-models")

	srv := NewLlamaServer(config.NewSource(cfg))

	err := srv.Start()
	if err == nil {
		t.Fatal("missing model must fail")
	}

	if got := srv.State(); got != StateFailed {
		t.Fatalf("failed boot must report failed, got %s", got)
	}
}

func TestEngineStopWalksStoppingToStopped(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := srv.State(); got != StateStopped {
		t.Fatalf("after Stop the engine must be stopped, got %s", got)
	}

	if srv.IsRunning() {
		t.Fatal("stopped engine must not report running")
	}

	if srv.Pid() != 0 {
		t.Fatal("stopped engine must not report a pid")
	}
}

func TestEngineEventsArePublished(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.After(5 * time.Second)

	var sawReady bool

	for !sawReady {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed unexpectedly")
			}

			if ev.State == StateReady {
				sawReady = true
			}

		case <-deadline:
			t.Fatal("timed out waiting for a ready event")
		}
	}
}

func TestEngineDeathTriggersBoundedAutoRestart(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "crash")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	// The fake engine dies ~500ms after becoming healthy. The watchdog must
	// restart it (bounded), reaching ready again — real recovery, no stale
	// running state.
	deadline := time.After(20 * time.Second)

	restarts := atomic.Int32{}

	lastState := ""

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed")
			}

			lastState = ev.State

			if ev.State == StateReady {
				restarts.Add(1)

				if restarts.Load() >= 2 {
					// Initial ready + at least one post-crash ready.
					t.Logf("recovered to ready after crash (last=%s)", lastState)

					return
				}
			}

		case <-deadline:
			t.Fatalf("engine did not recover after crash (restarts=%d state=%s)", restarts.Load(), lastState)
		}
	}
}

func TestMarkBusyFlipsReadyAndBusy(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	// Idle engine: busy reporting must be a no-op.
	srv.MarkBusy(true)

	if got := srv.State(); got != StateIdle {
		t.Fatalf("busy on idle engine must be a no-op, got %s", got)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	srv.MarkBusy(true)

	if got := srv.State(); got != StateBusy {
		t.Fatalf("engine must be busy during inference, got %s", got)
	}

	if !srv.IsRunning() {
		t.Fatal("busy engine is still alive")
	}

	srv.MarkBusy(false)

	if got := srv.State(); got != StateReady {
		t.Fatalf("engine must return to ready after inference, got %s", got)
	}
}

func TestStopWithoutProcessIsSafe(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop on never-started engine: %v", err)
	}

	if got := srv.State(); got != StateStopped {
		t.Fatalf("expected stopped, got %s", got)
	}
}

func TestResolveModelPathPicksFirstAvailable(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(dir+"/model-a.gguf", []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dir+"/model-b.gguf", []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := ResolveModelPath(dir, "")
	if err != nil {
		t.Fatalf("resolve without name: %v", err)
	}

	if !strings.HasSuffix(path, ".gguf") {
		t.Fatalf("unexpected path %s", path)
	}

	// Exact (case-insensitive) match.
	path, err = ResolveModelPath(dir, "MODEL-B.GGUF")
	if err != nil {
		t.Fatalf("resolve exact: %v", err)
	}

	if !strings.Contains(path, "model-b.gguf") {
		t.Fatalf("unexpected exact resolve: %s", path)
	}

	// Unknown name must fail with a helpful message.
	_, err = ResolveModelPath(dir, "does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("unknown model must fail: %v", err)
	}
}

// compile-time guard: exec used by helper re-exec through proc package.
var _ = exec.Command

// ---------------------------------------------------------------------------
// v1.2.5 engine lifecycle contract tests (watchdog ownership + honest
// exit classification). These pin the invariants from the repair brief:
//
//	1. unexpected exit triggers restart          (bounded recovery)
//	2. deliberate stop does not restart          (no false crash)
//	3. stop during backoff cancels the restart   (cancelable watchdog)
//	4. repeated crashes exhaust the budget       (terminal failed)
//	5. stale restart cannot resurrect an engine  (generation guard)
//	6. budget resets only after a stable episode (crash loops terminate)
//	7. shutdown leaves no watchdog goroutine     (deterministic ownership)
// ---------------------------------------------------------------------------

// waitForState drains the subscription channel until one of the wanted
// states arrives (or the deadline passes).
func waitForState(t *testing.T, events <-chan EngineEvent, deadline time.Duration, wanted ...string) (EngineEvent, bool) {
	t.Helper()

	timer := time.After(deadline)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return EngineEvent{}, false
			}

			for _, w := range wanted {
				if ev.State == w {
					return ev, true
				}
			}

		case <-timer:
			return EngineEvent{}, false
		}
	}
}

func TestUnexpectedExitReportsDiagnosticDetail(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "crash")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
		t.Fatal("engine never became ready")
	}

	// The fake dies ~500ms after becoming healthy. The honest contract:
	// the detail exposes what happened, not a generic failure.
	deadline := time.Now().Add(10 * time.Second)

	for {
		if d := srv.Detail(); strings.Contains(d, "Unexpected exit") {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("detail never reported the unexpected exit: %q", srv.Detail())
		}

		time.Sleep(25 * time.Millisecond)
	}

	// Recovery: a fresh ready episode clears the recovery detail — the
	// engine is ready again, verified, and no longer "restarting".
	if _, ok := waitForState(t, events, 15*time.Second, StateReady); !ok {
		t.Fatal("engine never recovered to ready after the crash")
	}

	if d := srv.Detail(); strings.Contains(d, "restarting") {
		t.Fatalf("recovered engine still reports a restart in flight: %q", d)
	}
}

func TestDeliberateStopDoesNotRestart(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
		t.Fatal("engine never became ready")
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := srv.State(); got != StateStopped {
		t.Fatalf("deliberate stop must end stopped, got %s", got)
	}

	if got := srv.Restarts(); got != 0 {
		t.Fatalf("deliberate stop must not count a restart, got %d", got)
	}

	// No delayed watchdog may bring the engine back: no starting/ready
	// event may arrive after the deliberate stop.
	if ev, ok := waitForState(t, events, 2*time.Second,
		StateStarting, StateReady, StateRunning, StateBusy, StateDownloading); ok {
		t.Fatalf("engine resurrected after a deliberate stop: %s (detail %q)", ev.State, ev.Detail)
	}
}

func TestStopDuringRestartBackoffCancelsRestart(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "crash")

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, ok := waitForState(t, events, 10*time.Second, StateReady); !ok {
		t.Fatal("engine never became ready")
	}

	// Wait for the crash (stopped) — the watchdog is now in its 1s backoff.
	if _, ok := waitForState(t, events, 10*time.Second, StateStopped); !ok {
		t.Fatal("crashed engine never reported stopped")
	}

	// Deliberate stop DURING the backoff must cancel the pending restart.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop during backoff: %v", err)
	}

	if got := srv.State(); got != StateStopped {
		t.Fatalf("state after stop-during-backoff = %s, want stopped", got)
	}

	// The 1s watchdog would have fired well inside this window if the
	// cancellation leaked: no boot may follow.
	if ev, ok := waitForState(t, events, 2500*time.Millisecond,
		StateStarting, StateReady, StateRunning, StateBusy, StateDownloading); ok {
		t.Fatalf("pending restart was not canceled by Stop: %s", ev.State)
	}
}

func TestRepeatedCrashesExhaustBoundedBudget(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "crash")

	srv := NewLlamaServer(config.NewSource(cfg))

	events, unsubscribe := srv.SubscribeEvents()
	defer unsubscribe()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The fake crashes ~500ms after every healthy boot. Backoff 1s+2s+4s
	// plus boot overhead must exhaust the budget in bounded time and land
	// in the terminal failed state — never loop forever.
	if _, ok := waitForState(t, events, 60*time.Second, StateFailed); !ok {
		t.Fatalf("crash loop never reached the terminal failed state (state=%s restarts=%d)",
			srv.State(), srv.Restarts())
	}

	if d := srv.Detail(); !strings.Contains(d, "recovery exhausted") {
		t.Fatalf("failed detail must say recovery is exhausted, got %q", d)
	}

	_ = srv.Stop()
}

func TestStaleWatchdogCannotResurrectOldEpisode(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))

	// Arm a watchdog for episode 0 (no engine has ever started — the
	// server is idle and the arming path is purely state-machine work).
	srv.scheduleAutoRestart("fake-model.gguf")

	srv.mu.Lock()
	armed := srv.watchArmed
	oldGen := srv.gen
	srv.mu.Unlock()

	if !armed {
		t.Fatal("watchdog must arm when a restart is scheduled")
	}

	// A NEW lifecycle episode begins (e.g. a manual start). The stale
	// watchdog from the old episode must stand down.
	srv.mu.Lock()
	srv.beginEpisodeLocked()
	newGen := srv.gen
	srv.mu.Unlock()

	if newGen != oldGen+1 {
		t.Fatalf("episode generation must advance, %d -> %d", oldGen, newGen)
	}

	// Wait past the 1s backoff: the stale watchdog must NOT have started
	// anything.
	time.Sleep(2200 * time.Millisecond)

	if got := srv.State(); got != StateIdle {
		t.Fatalf("stale watchdog resurrected the engine: state=%s, want idle", got)
	}
}

func TestRestartBudgetResetsOnlyAfterStableEpisode(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	// Shrink the stability window so the test proves both branches.
	old := restartBudgetResetAfter
	restartBudgetResetAfter = 150 * time.Millisecond
	t.Cleanup(func() { restartBudgetResetAfter = old })

	srv := NewLlamaServer(config.NewSource(cfg))

	// Episode 1.
	srv.mu.Lock()
	srv.beginEpisodeLocked()
	srv.restarts = 2
	srv.mu.Unlock()

	// Episode 2 starts IMMEDIATELY: the previous episode was far below
	// the stability window, so the budget must NOT reset.
	srv.mu.Lock()
	srv.beginEpisodeLocked()
	got := srv.restarts
	srv.mu.Unlock()

	if got != 2 {
		t.Fatalf("short-lived episode must keep the restart budget (got %d, want 2)", got)
	}

	// Let the episode live past the stability window, then begin episode 3:
	// a genuinely healthy episode resets the budget.
	time.Sleep(250 * time.Millisecond)

	srv.mu.Lock()
	srv.beginEpisodeLocked()
	got = srv.restarts
	srv.mu.Unlock()

	if got != 0 {
		t.Fatalf("stable episode must reset the restart budget (got %d, want 0)", got)
	}
}

func TestStopLeavesNoWatchdogGoroutineBehind(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")

	srv := NewLlamaServer(config.NewSource(cfg))

	srv.scheduleAutoRestart("fake-model.gguf")

	srv.mu.Lock()
	armed := srv.watchArmed
	srv.mu.Unlock()

	if !armed {
		t.Fatal("watchdog must be armed before Stop")
	}

	// Stop (no live process — the early path) must cancel the pending
	// watchdog AND wait for its goroutine to exit.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	srv.mu.Lock()
	armed = srv.watchArmed
	pendingStop := srv.watchStop != nil
	pendingDone := srv.watchDone != nil
	srv.mu.Unlock()

	if armed || pendingStop || pendingDone {
		t.Fatalf("watchdog survived shutdown: armed=%v pendingStop=%v pendingDone=%v",
			armed, pendingStop, pendingDone)
	}

	// watchDone was awaited inside Stop, so the goroutine has provably
	// exited — no delayed Start() can follow. Belt and braces: no boot.
	time.Sleep(1500 * time.Millisecond)

	if got := srv.State(); got != StateStopped {
		t.Fatalf("engine state after shutdown = %s, want stopped", got)
	}
}
