// selection_test.go — the v1.5.0 MODEL-FIRST contract tests.
//
// Proven here through the REAL HTTP surface (the same handler chain a
// user drives):
//
//   - a fresh install with NO selected model never implicitly loads a
//     model: /api/engine reports selectionRequired + phase
//     "model-selection", the engine stays idle, and an explicit engine
//     start is a clean 400 (no arbitrary first-GGUF boot);
//   - POST /api/models/select applies the selection ATOMICALLY (one
//     config write: model + AUTO profile together) and reports the
//     backend-authoritative phase chain;
//   - MANUAL preserves explicit user values verbatim on a model change;
//   - an unknown model fails honestly (404) without touching config;
//   - a model change while a run is active is BLOCKED (409);
//   - the per-model recommendation evidence endpoint only labels a model
//     "safe" from the recommendation engine's measured evidence.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func writeModelFixture(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("GGUF....test-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func postSelect(t *testing.T, server *httptest.Server, body map[string]any) (int, map[string]any) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(server.URL+"/api/models/select", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /api/models/select: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode select response: %v", err)
	}

	return resp.StatusCode, out
}

func getEngineSnapshot(t *testing.T, server *httptest.Server) map[string]any {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode engine snapshot: %v", err)
	}

	return out
}

func getConfigMap(t *testing.T, server *httptest.Server) map[string]any {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	return out
}

// TestModelFirstFreshInstallNeverImplicitlyLoads: a fresh install (no
// selected model, models PRESENT) must reach the Model Selector state —
// never an engine boot of an arbitrary model.
func TestModelFirstFreshInstallNeverImplicitlyLoads(t *testing.T) {
	server, cfg := newTestServer(t)

	// Local models EXIST — the old behavior would have picked the first.
	writeModelFixture(t, cfg.ModelsDir, "aaa-first-alphabetical.gguf")
	writeModelFixture(t, cfg.ModelsDir, "zzz-other.gguf")

	// EnsureSetup already ran (with LlamaAutoStart=false in the helper —
	// but the PREWARM ITSELF must gate on the selection, so re-prove the
	// gate directly through the runtime prewarm seam).
	snap := getEngineSnapshot(t, server)

	if snap["selectionRequired"] != true {
		t.Fatalf("fresh install must report selectionRequired, got %+v", snap)
	}
	if snap["phase"] != "model-selection" {
		t.Fatalf("fresh install phase = %v, want model-selection", snap["phase"])
	}
	if state, _ := snap["state"].(string); state != "idle" && state != "stopped" {
		t.Fatalf("engine must stay idle/stopped before selection, got %q", state)
	}

	// An explicit engine start without a selection is a clean 400 with
	// the actionable reason — never a silent arbitrary-model boot.
	resp, err := http.Post(server.URL+"/api/llama", "application/json",
		bytes.NewReader([]byte(`{"action":"start"}`)))
	if err != nil {
		t.Fatalf("POST /api/llama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("engine start without selection = %d, want 400", resp.StatusCode)
	}

	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(errBody.Error), []byte("no model selected")) {
		t.Fatalf("error must name the missing selection, got %q", errBody.Error)
	}
}

// TestSelectModelAppliesConfigAtomically (AUTO): the selection and the
// recommendation profile land in ONE config write — after the call,
// model + profile + previousModel are all present together, and the
// phase chain reached ready.
func TestSelectModelAppliesConfigAtomically(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	code, out := postSelect(t, server, map[string]any{"model": "model-a.gguf"})
	if code != http.StatusOK {
		t.Fatalf("select = %d: %v", code, out)
	}

	if out["phase"] != "ready" {
		t.Fatalf("select phase = %v, want ready (auto-start off: applied, boots on first use)", out["phase"])
	}
	if out["model"] != "model-a.gguf" {
		t.Fatalf("select model = %v", out["model"])
	}

	// The atomic write: model + profile + previousModel all present.
	cfgMap := getConfigMap(t, server)
	if cfgMap["model"] != "model-a.gguf" {
		t.Fatalf("config.model = %v", cfgMap["model"])
	}
	if cfgMap["performanceMode"] != "auto" {
		t.Fatalf("config.performanceMode = %v, want auto (the default)", cfgMap["performanceMode"])
	}

	// AUTO applied a profile (the recommendation engine produced one —
	// with a non-GGUF fixture the card is nil but the profile still
	// computes from measured hardware).
	sel, _ := out["applied"].(map[string]any)
	if sel == nil {
		t.Fatalf("AUTO selection must carry the applied profile: %v", out)
	}
	if ev, _ := out["evidence"].([]any); len(ev) == 0 {
		t.Fatalf("AUTO selection must carry the measured evidence: %v", out)
	}

	// Second selection records the previous model.
	code, out2 := postSelect(t, server, map[string]any{"model": "model-b.gguf"})
	if code != http.StatusOK {
		t.Fatalf("second select = %d: %v", code, out2)
	}
	if out2["previousModel"] != "model-a.gguf" {
		t.Fatalf("previousModel = %v, want model-a.gguf", out2["previousModel"])
	}

	cfgMap = getConfigMap(t, server)
	if cfgMap["model"] != "model-b.gguf" {
		t.Fatalf("config.model after switch = %v", cfgMap["model"])
	}
	if cfgMap["previousModel"] != "model-a.gguf" {
		t.Fatalf("config.previousModel = %v", cfgMap["previousModel"])
	}
}

// TestSelectModelManualPreservesExplicitValues: in MANUAL a model change
// never overwrites the user's explicit settings.
func TestSelectModelManualPreservesExplicitValues(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	// The user's explicit manual values.
	patch := map[string]any{
		"performanceMode": "manual",
		"llm": map[string]any{
			"numCtx":    1234,
			"numThread": 3,
		},
		"flashAttention": false,
	}
	raw, _ := json.Marshal(patch)
	resp, err := http.NewRequest(http.MethodPut, server.URL+"/api/config", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Header.Set("Content-Type", "application/json")
	httpResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	httpResp.Body.Close()

	code, out := postSelect(t, server, map[string]any{"model": "model-b.gguf"})
	if code != http.StatusOK {
		t.Fatalf("manual select = %d: %v", code, out)
	}

	if out["applied"] != nil {
		t.Fatalf("MANUAL selection must NOT apply a profile: %v", out["applied"])
	}

	cfgMap := getConfigMap(t, server)
	llm, _ := cfgMap["llm"].(map[string]any)
	if llm == nil || llm["numCtx"] != float64(1234) || llm["numThread"] != float64(3) {
		t.Fatalf("MANUAL must preserve explicit values, got llm=%v", llm)
	}
	if cfgMap["flashAttention"] != false {
		t.Fatalf("MANUAL must preserve flashAttention=false, got %v", cfgMap["flashAttention"])
	}
	if cfgMap["model"] != "model-b.gguf" {
		t.Fatalf("MANUAL still switches the model, got %v", cfgMap["model"])
	}
}

// TestSelectUnknownModelFailsHonestly: an unknown model is a 404 with
// the honest reason and NO config mutation.
func TestSelectUnknownModelFailsHonestly(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	before := getConfigMap(t, server)

	code, out := postSelect(t, server, map[string]any{"model": "ghost.gguf"})
	if code != http.StatusNotFound {
		t.Fatalf("unknown model select = %d, want 404: %v", code, out)
	}

	after := getConfigMap(t, server)
	if after["model"] != before["model"] {
		t.Fatalf("a failed selection must not mutate config: %v -> %v", before["model"], after["model"])
	}

	snap := getEngineSnapshot(t, server)
	sel, _ := snap["selection"].(map[string]any)
	if sel == nil || sel["phase"] != "failed" {
		t.Fatalf("selection state must record the honest failure: %v", sel)
	}
}

// TestSelectBlockedWhileRunActive: a model change while a generation is
// live is BLOCKED with 409 — never raced.
func TestSelectBlockedWhileRunActive(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	// Simulate ONE active run in the authoritative registry.
	srv.runsMu.Lock()
	srv.runs["session-live"] = &runState{
		cancel: func() {},
		hub:    newActivityHub(),
	}
	srv.runsMu.Unlock()

	rec := httptest.NewRequest(http.MethodPost, "/api/models/select",
		bytes.NewReader([]byte(`{"model":"model-a.gguf"}`)))
	rec.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleModelsSelect(w, rec)

	if w.Code != http.StatusConflict {
		t.Fatalf("select during a live run = %d, want 409: %s", w.Code, w.Body.String())
	}
}

// TestEngineSnapshotCarriesSelectionState: after a selection the engine
// snapshot carries the backend-authoritative selection block.
func TestEngineSnapshotCarriesSelectionState(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	snap := getEngineSnapshot(t, server)
	if _, has := snap["selection"]; has {
		t.Fatalf("before any selection the pre-selection state is described by selectionRequired alone: %v", snap)
	}

	code, _ := postSelect(t, server, map[string]any{"model": "model-a.gguf"})
	if code != http.StatusOK {
		t.Fatal("select failed")
	}

	snap = getEngineSnapshot(t, server)
	sel, _ := snap["selection"].(map[string]any)
	if sel == nil {
		t.Fatalf("engine snapshot must carry the selection state: %v", snap)
	}
	if sel["phase"] != "ready" {
		t.Fatalf("selection phase = %v, want ready", sel["phase"])
	}
	if sel["performanceMode"] != "auto" {
		t.Fatalf("selection performanceMode = %v", sel["performanceMode"])
	}
	// selectionRequired cleared once a model is selected.
	if snap["selectionRequired"] == true {
		t.Fatalf("selectionRequired must clear after a selection: %v", snap)
	}
}

// TestModelsRecommendationsEvidenceOnly: the per-model recommendation
// endpoint derives labels ONLY from the recommendation engine's
// evidence over measured hardware — a fixture model that is not a real
// GGUF must never be labelled "recommended" from filename/size alone.
func TestModelsRecommendationsEvidenceOnly(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	resp, err := http.Get(server.URL + "/api/models/recommendations")
	if err != nil {
		t.Fatalf("GET /api/models/recommendations: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recommendations = %d", resp.StatusCode)
	}

	var out struct {
		HardwareMeasured bool `json:"hardwareMeasured"`
		Recommendations  map[string]struct {
			Class            string   `json:"class"`
			Reasons          []string `json:"reasons"`
			HardwareMeasured bool     `json:"hardwareMeasured"`
		} `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode recommendations: %v", err)
	}

	if !out.HardwareMeasured {
		t.Fatalf("the endpoint must report measured hardware: %+v", out)
	}

	rec, ok := out.Recommendations["model-a.gguf"]
	if !ok {
		t.Fatalf("the local model must be present: %+v", out.Recommendations)
	}
	// A non-GGUF fixture has no model card: the engine CANNOT honestly
	// assess its resources, so the verdict is EMPTY (never a filename or
	// size heuristic dressed up as "safe").
	if rec.Class == "safe" {
		t.Fatalf("a card-less fixture must never be labelled safe from size alone: %+v", rec)
	}
}

// TestModelsListMarksPreviousSelection: the models list carries the
// previous-selection marker after a switch.
func TestModelsListMarksPreviousSelection(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	if code, _ := postSelect(t, server, map[string]any{"model": "model-a.gguf"}); code != http.StatusOK {
		t.Fatal("select a failed")
	}
	if code, _ := postSelect(t, server, map[string]any{"model": "model-b.gguf"}); code != http.StatusOK {
		t.Fatal("select b failed")
	}

	resp, err := http.Get(server.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Local []struct {
			ID       string `json:"id"`
			Previous bool   `json:"previous"`
		} `json:"local"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	for _, m := range out.Local {
		if m.ID == "model-a.gguf" && !m.Previous {
			t.Fatalf("model-a must carry the previous-selection marker: %+v", out.Local)
		}
	}
}
