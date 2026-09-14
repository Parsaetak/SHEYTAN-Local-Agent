import {
  useEffect,
  useMemo,
  useState,
  type ChangeEvent,
} from "react";

import {
  api,
  type LLMConfig,
  type Model,
  type PerfSnapshot,
  type Preset,
  type RuntimeConfig,
  type SysInfo,
  type ToolInfo,
} from "./api";

import { FieldLabel } from "./settings-shared";
import {
  ContextCard,
  EngineCard,
  EngineProfileCard,
  LiveMetricsCard,
  LogsCard,
  NetworkCard,
  RecommendedCard,
  type PerfBaseline,
} from "./SettingsPerformance";

type SaveState = "idle" | "loading" | "saved" | "error";

// v1.1.7: Settings is organised into scannable sections instead of one
// long page. Every section keeps the same card system, theme tokens and
// scrolling behaviour as before.
type SettingsTab =
  | "general"
  | "models"
  | "performance"
  | "generation"
  | "tools"
  | "network"
  | "logs"
  | "advanced";

const TABS: { id: SettingsTab; label: string }[] = [
  { id: "general", label: "General" },
  { id: "models", label: "Models" },
  { id: "performance", label: "Performance" },
  { id: "generation", label: "Generation" },
  { id: "tools", label: "Tools" },
  { id: "network", label: "Network" },
  { id: "logs", label: "Logs" },
  { id: "advanced", label: "Advanced" },
];

// Engine-affecting top-level keys: saving any of these restarts the local
// engine so the option ACTUALLY takes effect (v1.1.7 — the previous list
// silently left speed options inert until a manual restart).
const ENGINE_KEYS = [
  "flashAttention",
  "cacheReuse",
  "ubatchSize",
  "threadsBatch",
  "kvCacheQuant",
  "mlock",
  "gpuAutoOffload",
  "draftModel",
] as const;

function touchesEngine(patch: Record<string, unknown>): boolean {
  if (ENGINE_KEYS.some((key) => patch[key] !== undefined)) {
    return true;
  }
  return (
    patch.model !== undefined ||
    patch.llamaBinPath !== undefined ||
    patch.llamaHost !== undefined ||
    patch.llamaPort !== undefined ||
    patch.llm !== undefined
  );
}

function numberValue(event: ChangeEvent<HTMLInputElement>): number {
  const value = Number(event.target.value);
  return Number.isFinite(value) ? value : 0;
}

function SettingsPanel() {
  const [config, setConfig] = useState<RuntimeConfig | null>(null);
  const [models, setModels] = useState<Model[]>([]);
  const [presets, setPresets] = useState<Preset[]>([]);
  // v1.1.5Z Phase 6: the backend currently serving generation ("native" |
  // "llama") — reported by /api/models so the local model list can mark the
  // serving model honestly instead of guessing from llamaRunning.
  const [servingBackend, setServingBackend] = useState<string>("");
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [sysinfo, setSysinfo] = useState<SysInfo | null>(null);

  const [saveState, setSaveState] = useState<SaveState>("loading");
  const [error, setError] = useState<string | null>(null);
  const [restartAfterSave, setRestartAfterSave] = useState(true);

  // v1.1.7: active settings section.
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");

  // v1.1.7: live performance snapshot (polled while the Performance tab is
  // open) and the optional before/after comparison baseline.
  const [perf, setPerf] = useState<PerfSnapshot | null>(null);
  const [baseline, setBaseline] = useState<PerfBaseline | null>(null);

  async function load() {
    setSaveState("loading");
    setError(null);

    try {
      const [, modelResponse, nextPresets, nextTools, nextSysinfo] =
        await Promise.all([
          api.config(),
          api.models(),
          api.presets(),
          api.tools(),
          api.sysinfo(),
        ]);

      // v1.1.2Z hardening: the API contract guarantees arrays, but a stale
      // backend or an interrupted deploy could still surface null — never
      // let a null list crash the whole React tree again.
      setModels(modelResponse.local ?? []);
      // v1.1.5Z Phase 6: honest engine status — remember which backend
      // serves generation so the model list can say "currently serving"
      // truthfully instead of guessing from llamaRunning.
      setServingBackend(modelResponse.backend ?? "");
      setPresets(nextPresets ?? []);
      setTools(nextTools ?? []);
      setSysinfo(nextSysinfo);
      setSaveState("idle");
    } catch (loadError) {
      setError(
        loadError instanceof Error
          ? loadError.message
          : "Unable to load runtime settings.",
      );
      setSaveState("error");
    }
  }

  useEffect(() => {
    void load();
  }, []);

  // v1.1.7: lightweight live-metrics polling — ONLY while the Performance
  // tab is visible, at a human cadence (3 s), so the app stays quiet
  // everywhere else.
  useEffect(() => {
    if (activeTab !== "performance") {
      return;
    }

    let cancelled = false;

    async function tick() {
      try {
        const snapshot = await api.perf();
        if (!cancelled) {
          setPerf(snapshot);
        }
      } catch {
        // Engine restart or backend hiccup — keep the last honest view.
      }
    }

    void tick();
    const timer = window.setInterval(tick, 3000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeTab]);

  async function save(patch: Record<string, unknown>) {
    if (!config) {
      return;
    }

    // v1.1.7 P1: before/after observation — when a performance-relevant
    // option changes, remember the CURRENT measured metrics so the Live
    // Metrics card can show an honest Before → After comparison once the
    // next generation runs. In-memory only; no benchmark database.
    const perfKeysTouched =
      patch.llm !== undefined ||
      ENGINE_KEYS.some((key) => patch[key] !== undefined);

    if (perfKeysTouched && perf?.generation?.hasSample) {
      setBaseline({
        label: "setting change",
        at: new Date().toLocaleTimeString(),
        genTokPerSec: perf.generation.genTokPerSec,
        promptTokPerSec: perf.generation.promptTokPerSec,
        ttftSeconds: perf.generation.ttftSeconds,
        cpuPercent: perf.cpuPercent ?? null,
        gpuPercent: perf.gpuPercent ?? null,
      });
    }

    setSaveState("loading");
    setError(null);

    try {
      const nextConfig = await api.updateConfig(patch);
      setConfig(nextConfig);

      // v1.1.7: the restart condition now covers every engine-affecting
      // option, so speed settings genuinely apply instead of waiting for a
      // manual restart.
      if (restartAfterSave && touchesEngine(patch)) {
        try {
          await api.llama("stop");
        } catch {
          // Engine may already be stopped.
        }

        await api.llama("start");
      }

      setSaveState("saved");
    } catch (saveError) {
      setError(
        saveError instanceof Error
          ? saveError.message
          : "Unable to save settings.",
      );
      setSaveState("error");
    }
  }

  async function selectModel(model: string) {
    if (!model || model === config?.model) {
      return;
    }

    await save({
      model,
    });
  }

  async function selectProvider(provider: string) {
    await save({
      provider,
    });
  }

  async function applyPreset(preset: Preset) {
    const llm: Partial<LLMConfig> = {
      ...(typeof preset.temperature === "number"
        ? { temperature: preset.temperature }
        : {}),
      ...(typeof preset.top_p === "number" ? { topP: preset.top_p } : {}),
      ...(typeof preset.top_k === "number" ? { topK: preset.top_k } : {}),
      ...(typeof preset.max_tokens === "number"
        ? { maxTokens: preset.max_tokens }
        : {}),
      ...(typeof preset.repeat_penalty === "number"
        ? { repeatPenalty: preset.repeat_penalty }
        : {}),
      ...(typeof preset.mirostat === "number" ? { mirostat: preset.mirostat } : {}),
      ...(typeof preset.num_ctx === "number" ? { numCtx: preset.num_ctx } : {}),
      preset: preset.id,
    };

    await save({
      llm,
    });
  }

  async function toggleTool(name: string, enabled: boolean) {
    const current = config?.enabledTools ?? [];

    const active =
      current.length === 0 ? tools.map((tool) => tool.name) : [...current];

    const next = enabled
      ? Array.from(new Set([...active, name]))
      : active.filter((item) => item !== name);

    await save({
      enabledTools: next,
    });
  }

  function updateLocalLLM<K extends keyof LLMConfig>(
    key: K,
    value: LLMConfig[K],
  ) {
    setConfig((current) =>
      current
        ? {
            ...current,
            llm: {
              ...current.llm,
              [key]: value,
            },
          }
        : current,
    );
  }

  // v1.1.7: top-level config draft updater for the Performance cards.
  function updateConfigLocal<K extends keyof RuntimeConfig>(
    key: K,
    value: RuntimeConfig[K],
  ) {
    setConfig((current) =>
      current ? { ...current, [key]: value } : current,
    );
  }

  const currentPreset = useMemo(
    () => presets.find((preset) => preset.id === config?.llm.preset),
    [presets, config?.llm.preset],
  );

  const effectiveTools = useMemo(
    () =>
      config?.enabledTools?.length
        ? new Set(config.enabledTools)
        : new Set(tools.map((tool) => tool.name)),
    [config?.enabledTools, tools],
  );

  if (!config) {
    return (
      <div className="settings-page" tabIndex={-1}>
        <div className="settings-loading">
          <div className="panel-loading-mark">✦</div>
          <strong>Loading runtime configuration</strong>
          <span>{error ?? "Reading local runtime state…"}</span>
          {error ? (
            <button
              type="button"
              className="secondary-button"
              onClick={() => void load()}
            >
              Retry
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div className="settings-page">
      <div className="settings-toolbar">
        <div>
          <span className="eyebrow">RUNTIME CONTROL</span>
          <h2>Configure SHEYTAN</h2>
          <p>
            Models, inference, agent behavior, tools, browser automation,
            Coding Lab, research, and performance.
          </p>
        </div>

        <div className="settings-toolbar-actions">
          <label className="inline-toggle">
            <input
              type="checkbox"
              checked={restartAfterSave}
              onChange={(event) => setRestartAfterSave(event.target.checked)}
            />
            <span>Restart engine after engine changes</span>
          </label>

          <button
            type="button"
            className="secondary-button"
            onClick={() => void load()}
            disabled={saveState === "loading"}
          >
            Refresh
          </button>
        </div>
      </div>

      {/* v1.1.7: section navigation — scan first, scroll less. */}
      <div className="settings-tabs" role="tablist">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={activeTab === tab.id}
            className={`settings-tab ${activeTab === tab.id ? "active" : ""}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {saveState === "saved" ? (
        <div className="settings-status success">
          Settings saved successfully.
        </div>
      ) : null}

      {error ? (
        <div className="settings-status error">
          {error}
        </div>
      ) : null}

      <div className="settings-grid">
        {/* ======================================================== */}
        {/* GENERAL */}
        {/* ======================================================== */}
        {/* v1.1.8: Models gets its own tab — model/provider identity is
            its own intent, not a "General"Misc item. */}
        {activeTab === "models" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">PROVIDER</span>
                  <h3>Model runtime</h3>
                </div>
                <span className="settings-card-value">{config.provider}</span>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Provider"
                    tip="Local runs models on this machine with llama.cpp; Remote talks to an OpenAI-compatible API over the internet. Local keeps everything private."
                  />
                  <select
                    value={config.provider}
                    onChange={(event) => void selectProvider(event.target.value)}
                  >
                    <option value="local">Local GGUF / llama.cpp</option>
                    <option value="remote">Remote OpenAI-compatible API</option>
                  </select>
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Local model"
                    tip="Which GGUF model file to load into the local engine. The currently serving model is marked automatically."
                  />
                  <select
                    value={config.model}
                    onChange={(event) => void selectModel(event.target.value)}
                    disabled={config.provider !== "local"}
                  >
                    <option value="">Select a local model</option>
                    {models.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                        {model.serving
                          ? ` — currently serving${
                              servingBackend === "native" ? " (native engine)" : " (llama.cpp)"
                            }`
                          : ""}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="settings-field">
                  <span>LLM base URL</span>
                  <input
                    value={config.llmBaseUrl}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              llmBaseUrl: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() => void save({ llmBaseUrl: config.llmBaseUrl })}
                  />
                </label>

                <label className="settings-field">
                  <span>Models directory</span>
                  <input
                    value={config.modelsDir}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              modelsDir: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() => void save({ modelsDir: config.modelsDir })}
                  />
                </label>

                <label className="settings-field">
                  <span>Remote base URL</span>
                  <input
                    value={config.remoteBaseUrl}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              remoteBaseUrl: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ remoteBaseUrl: config.remoteBaseUrl })
                    }
                    disabled={config.provider !== "remote"}
                  />
                </label>

                <label className="settings-field">
                  <span>Remote model</span>
                  <input
                    value={config.remoteModel}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              remoteModel: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() => void save({ remoteModel: config.remoteModel })}
                    disabled={config.provider !== "remote"}
                  />
                </label>

                <div className="settings-note">
                  Remote API keys remain redacted by the backend and are not
                  rendered into the UI.
                </div>
              </div>
            </section>
          </>
        ) : null}

        {activeTab === "general" ? (
          <>
            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">AGENT</span>
                  <h3>Behavior</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Maximum iterations"
                    tip="How many tool steps the agent may take per task before it must report back. Higher allows longer autonomous work."
                  />
                  <input
                    type="number"
                    min="1"
                    max="1000"
                    value={config.maxIterations}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current ? { ...current, maxIterations: value } : current,
                      );
                    }}
                    onBlur={() => void save({ maxIterations: config.maxIterations })}
                  />
                </label>

                <label className="settings-field">
                  <span>Recall Top K</span>
                  <input
                    type="number"
                    min="0"
                    max="100"
                    value={config.recallTopK}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current ? { ...current, recallTopK: value } : current,
                      );
                    }}
                    onBlur={() => void save({ recallTopK: config.recallTopK })}
                  />
                </label>

                <label className="inline-toggle">
                  <FieldLabel
                    name="Thinking mode"
                    tip="Lets the model reason privately before answering — better on hard tasks, slightly slower."
                  />
                  <input
                    type="checkbox"
                    checked={config.thinkingMode}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, thinkingMode: value } : current,
                      );

                      void save({ thinkingMode: value });
                    }}
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.parallelTools}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, parallelTools: value } : current,
                      );

                      void save({ parallelTools: value });
                    }}
                  />
                  <span>Parallel tools</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.recallEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, recallEnabled: value } : current,
                      );

                      void save({ recallEnabled: value });
                    }}
                  />
                  <span>Memory recall</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.continuumEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, continuumEnabled: value }
                          : current,
                      );

                      void save({ continuumEnabled: value });
                    }}
                  />
                  <span>Continuum context</span>
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">BROWSER</span>
                  <h3>Automation</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <span>Browser executable</span>
                  <input
                    value={config.browserExecutablePath}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              browserExecutablePath: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        browserExecutablePath: config.browserExecutablePath,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>Slow-mo (ms)</span>
                  <input
                    type="number"
                    min="0"
                    value={config.browserSlowMoMs}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              browserSlowMoMs: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ browserSlowMoMs: config.browserSlowMoMs })
                    }
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.browserHeadless}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, browserHeadless: value } : current,
                      );

                      void save({ browserHeadless: value });
                    }}
                  />
                  <span>Headless browser</span>
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">VISION</span>
                  <h3>Multimodal</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.visionEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, visionEnabled: value } : current,
                      );

                      void save({ visionEnabled: value });
                    }}
                  />
                  <span>Enable vision</span>
                </label>

                <label className="settings-field">
                  <span>MM projector</span>
                  <input
                    value={config.visionMmproj}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, visionMmproj: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ visionMmproj: config.visionMmproj })
                    }
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">SANDBOX</span>
                  <h3>Execution</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.sandboxEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, sandboxEnabled: value } : current,
                      );

                      void save({ sandboxEnabled: value });
                    }}
                  />
                  <span>Enable sandbox</span>
                </label>

                <label className="settings-field">
                  <span>Memory limit</span>
                  <input
                    value={config.sandboxMemory}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, sandboxMemory: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ sandboxMemory: config.sandboxMemory })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>CPU limit</span>
                  <input
                    type="number"
                    min="1"
                    value={config.sandboxCPU}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, sandboxCPU: numberValue(event) }
                          : current,
                      )
                    }
                    onBlur={() => void save({ sandboxCPU: config.sandboxCPU })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">CODING LAB</span>
                  <h3>Autonomous engineering</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.labEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, labEnabled: value } : current,
                      );

                      void save({ labEnabled: value });
                    }}
                  />
                  <span>Enable Coding Lab</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.labKeepWorkspaces}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, labKeepWorkspaces: value }
                          : current,
                      );

                      void save({ labKeepWorkspaces: value });
                    }}
                  />
                  <span>Keep completed workspaces</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.labAllowNetwork}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, labAllowNetwork: value }
                          : current,
                      );

                      void save({ labAllowNetwork: value });
                    }}
                  />
                  <span>Allow laboratory network</span>
                </label>

                <label className="settings-field">
                  <span>Workspace root</span>
                  <input
                    value={config.labWorkspaceRoot}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? { ...current, labWorkspaceRoot: event.target.value }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({ labWorkspaceRoot: config.labWorkspaceRoot })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>Command timeout (sec)</span>
                  <input
                    type="number"
                    min="1"
                    value={config.labCommandTimeoutSec}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              labCommandTimeoutSec: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        labCommandTimeoutSec: config.labCommandTimeoutSec,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>Lab iterations</span>
                  <input
                    type="number"
                    min="1"
                    value={config.labMaxIterations}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              labMaxIterations: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        labMaxIterations: config.labMaxIterations,
                      })
                    }
                  />
                </label>
              </div>
            </section>

            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">RESEARCH</span>
                  <h3>External intelligence</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.researchEnabled}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current
                          ? { ...current, researchEnabled: value }
                          : current,
                      );

                      void save({ researchEnabled: value });
                    }}
                  />
                  <span>Enable research</span>
                </label>

                <label className="settings-field">
                  <span>Backend</span>
                  <select
                    value={config.researchBackend}
                    onChange={(event) => {
                      const value = event.target.value;

                      setConfig((current) =>
                        current
                          ? { ...current, researchBackend: value }
                          : current,
                      );

                      void save({ researchBackend: value });
                    }}
                  >
                    <option value="auto">Automatic</option>
                    <option value="searxng">SearXNG</option>
                    <option value="duckduckgo">DuckDuckGo</option>
                  </select>
                </label>

                <label className="settings-field">
                  <span>SearXNG URL</span>
                  <input
                    value={config.researchSearxngUrl}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchSearxngUrl: event.target.value,
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchSearxngUrl: config.researchSearxngUrl,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>Maximum results</span>
                  <input
                    type="number"
                    min="1"
                    max="100"
                    value={config.researchMaxResults}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchMaxResults: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchMaxResults: config.researchMaxResults,
                      })
                    }
                  />
                </label>

                <label className="settings-field">
                  <span>Timeout (sec)</span>
                  <input
                    type="number"
                    min="1"
                    value={config.researchTimeoutSec}
                    onChange={(event) =>
                      setConfig((current) =>
                        current
                          ? {
                              ...current,
                              researchTimeoutSec: numberValue(event),
                            }
                          : current,
                      )
                    }
                    onBlur={() =>
                      void save({
                        researchTimeoutSec: config.researchTimeoutSec,
                      })
                    }
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.researchGitHub}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, researchGitHub: value } : current,
                      );

                      void save({ researchGitHub: value });
                    }}
                  />
                  <span>GitHub research</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.researchReddit}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, researchReddit: value } : current,
                      );

                      void save({ researchReddit: value });
                    }}
                  />
                  <span>Reddit research</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.researchWeb}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, researchWeb: value } : current,
                      );

                      void save({ researchWeb: value });
                    }}
                  />
                  <span>General web research</span>
                </label>
              </div>
            </section>
          </>
        ) : null}

        {/* v1.1.8: Advanced collects host/diagnostic detail that does not
            need to sit in the middle of everyday settings. */}
        {activeTab === "advanced" ? (
          <>
            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">HARDWARE</span>
                  <h3>System profile</h3>
                </div>
              </div>

              {sysinfo ? (
                <div className="hardware-grid">
                  <div>
                    <span>CPU</span>
                    <strong>{sysinfo.cpu.name}</strong>
                  </div>

                  <div>
                    <span>Threads</span>
                    <strong>{sysinfo.cpu.logicalCores}</strong>
                  </div>

                  <div>
                    <span>RAM</span>
                    <strong>
                      {(sysinfo.ram.totalBytes / 1024 / 1024 / 1024).toFixed(1)} GB
                    </strong>
                  </div>

                  <div>
                    <span>Disk free</span>
                    <strong>
                      {(sysinfo.disk.freeBytes / 1024 / 1024 / 1024).toFixed(1)} GB
                    </strong>
                  </div>

                  <div>
                    <span>Recommended context</span>
                    <strong>{sysinfo.recommended.numCtx}</strong>
                  </div>

                  <div>
                    <span>Recommended batch</span>
                    <strong>{sysinfo.recommended.numBatch}</strong>
                  </div>
                </div>
              ) : (
                <span className="settings-note">Hardware information unavailable.</span>
              )}

              {sysinfo?.recommended.warnings?.length ? (
                <div className="hardware-warnings">
                  {sysinfo.recommended.warnings.map((warning) => (
                    <div key={warning}>{warning}</div>
                  ))}
                </div>
              ) : null}
            </section>
          </>
        ) : null}

        {/* ======================================================== */}
        {/* PERFORMANCE: Engine profile → Recommended → Engine → */}
        {/* Context → Live Metrics */}
        {/* ======================================================== */}
        {activeTab === "performance" ? (
          <>
            <EngineProfileCard perf={perf} />

            <RecommendedCard config={config} perf={perf} save={save} />

            <EngineCard
              config={config}
              perf={perf}
              save={save}
              updateLocalLLM={updateLocalLLM}
              updateConfigLocal={updateConfigLocal}
            />

            <ContextCard
              config={config}
              perf={perf}
              save={save}
              updateLocalLLM={updateLocalLLM}
              updateConfigLocal={updateConfigLocal}
            />

            <LiveMetricsCard
              perf={perf}
              baseline={baseline}
              clearBaseline={() => setBaseline(null)}
            />
          </>
        ) : null}

        {/* ======================================================== */}
        {/* GENERATION */}
        {/* ======================================================== */}
        {activeTab === "generation" ? (
          <>
            <section className="settings-card settings-card-wide">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">PRESETS</span>
                  <h3>Generation profile</h3>
                </div>
                <span className="settings-card-value">
                  {currentPreset?.label ?? currentPreset?.name ?? config.llm.preset}
                </span>
              </div>

              <div className="preset-grid">
                {presets.map((preset) => {
                  const label = preset.label ?? preset.name ?? preset.id;

                  return (
                    <button
                      type="button"
                      key={preset.id}
                      className={`preset-card ${
                        preset.id === config.llm.preset ? "active" : ""
                      }`}
                      onClick={() => void applyPreset(preset)}
                    >
                      <strong>{label}</strong>
                      <span>{preset.description ?? "Runtime preset"}</span>
                    </button>
                  );
                })}
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">SAMPLING</span>
                  <h3>Generation</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <FieldLabel
                    name="Temperature"
                    tip="How creative the model may be. Low is precise and predictable; high is varied and sometimes erratic. 0.7 suits most work."
                  />
                  <input
                    type="number"
                    min="0"
                    max="2"
                    step="0.05"
                    value={config.llm.temperature}
                    onChange={(event) =>
                      updateLocalLLM("temperature", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Top P"
                    tip="Keeps only the most likely words whose probabilities add up to this share. Lower = safer wording."
                  />
                  <input
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={config.llm.topP}
                    onChange={(event) =>
                      updateLocalLLM("topP", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Top K</span>
                  <input
                    type="number"
                    min="0"
                    step="1"
                    value={config.llm.topK}
                    onChange={(event) =>
                      updateLocalLLM("topK", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Min P</span>
                  <input
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={config.llm.minP}
                    onChange={(event) =>
                      updateLocalLLM("minP", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <FieldLabel
                    name="Max tokens"
                    tip="The longest single reply the model may produce. Higher allows long answers; it does not make short answers faster."
                  />
                  <input
                    type="number"
                    min="1"
                    step="128"
                    value={config.llm.maxTokens}
                    onChange={(event) =>
                      updateLocalLLM("maxTokens", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Repeat penalty</span>
                  <input
                    type="number"
                    min="0.5"
                    max="2"
                    step="0.01"
                    value={config.llm.repeatPenalty}
                    onChange={(event) =>
                      updateLocalLLM("repeatPenalty", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>

                <label className="settings-field">
                  <span>Seed</span>
                  <input
                    type="number"
                    step="1"
                    value={config.llm.seed}
                    onChange={(event) =>
                      updateLocalLLM("seed", numberValue(event))
                    }
                    onBlur={() => void save({ llm: config.llm })}
                  />
                </label>
              </div>
            </section>

            <section className="settings-card">
              <div className="settings-card-heading">
                <div>
                  <span className="eyebrow">INTERFACE</span>
                  <h3>Runtime pacing</h3>
                </div>
              </div>

              <div className="settings-form-grid">
                <label className="settings-field">
                  <span>Target FPS</span>
                  <input
                    type="number"
                    min="30"
                    max="240"
                    value={config.targetFps}
                    onChange={(event) => {
                      const value = numberValue(event);

                      setConfig((current) =>
                        current ? { ...current, targetFps: value } : current,
                      );
                    }}
                    onBlur={() => void save({ targetFps: config.targetFps })}
                  />
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.smoothStream}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, smoothStream: value } : current,
                      );

                      void save({ smoothStream: value });
                    }}
                  />
                  <span>Smooth token streaming</span>
                </label>

                <label className="inline-toggle">
                  <input
                    type="checkbox"
                    checked={config.showPerfHud}
                    onChange={(event) => {
                      const value = event.target.checked;

                      setConfig((current) =>
                        current ? { ...current, showPerfHud: value } : current,
                      );

                      void save({ showPerfHud: value });
                    }}
                  />
                  <span>Performance HUD (frame timing, dev tool)</span>
                </label>
              </div>
            </section>
          </>
        ) : null}

        {/* ======================================================== */}
        {/* TOOLS — concise one-line descriptions; the full */}
        {/* operational specs drive the model itself. */}
        {/* ======================================================== */}
        {activeTab === "tools" ? (
          <section className="settings-card settings-card-wide">
            <div className="settings-card-heading">
              <div>
                <span className="eyebrow">TOOLS</span>
                <h3>Tool access</h3>
              </div>
              <span className="settings-card-value">{tools.length}</span>
            </div>

            <div className="tool-list">
              {tools.map((tool) => {
                const enabled = effectiveTools.has(tool.name);

                return (
                  <label key={tool.name} className="tool-row">
                    <span>
                      <strong>{tool.name}</strong>
                      <small>
                        {tool.description ?? "Agent tool"}
                        {tool.detail ? (
                          <span
                            className="settings-tip tool-detail-tip"
                            data-tip="Full operational documentation is embedded in the agent's tool spec — hover disabled on purpose to keep this list scannable."
                            tabIndex={0}
                            aria-label="Detailed documentation available in the agent tool spec"
                          >
                            ?
                          </span>
                        ) : null}
                      </small>
                    </span>

                    <input
                      type="checkbox"
                      checked={enabled}
                      onChange={(event) =>
                        void toggleTool(tool.name, event.target.checked)
                      }
                    />
                  </label>
                );
              })}
            </div>

            <span className="settings-note">
              Each line is the short label; the complete tool documentation
              still travels with the agent's own tool specifications.
            </span>
          </section>
        ) : null}

        {/* ======================================================== */}
        {/* NETWORK */}
        {/* ======================================================== */}
        {activeTab === "network" ? <NetworkCard /> : null}

        {/* ======================================================== */}
        {/* LOGS / DIAGNOSTICS */}
        {/* ======================================================== */}
        {activeTab === "logs" ? <LogsCard /> : null}
      </div>
    </div>
  );
}

export default SettingsPanel;
