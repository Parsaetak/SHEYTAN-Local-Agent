package llm

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	modelPath := dir + "/models"
	if err := os.MkdirAll(modelPath, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}

	modelFile := modelPath + "/fake-model.gguf"
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

	if got := srv.LoadedModel(); got != modelFile {
		t.Fatalf("loaded model = %q, want %q", got, modelFile)
	}

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
	cfg.ModelsDir = dir + "/empty-models"

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
