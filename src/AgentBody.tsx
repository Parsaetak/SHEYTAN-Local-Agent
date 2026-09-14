import {
  type ChangeEvent,
  type FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { api, type EngineState, type RuntimeConfig } from "./api";
import { initializeAgent } from "./agent-init";
import MessageStream, { AttachmentChip } from "./MessageStream";
import ActivityStream from "./ActivityStream";
import ModelPicker from "./ModelPicker";
import PerfStrip from "./PerfStrip";
import { useRuntimeStore } from "./store";

// engineBadge maps the authoritative backend engine states to a visible
// label + severity. The UI NEVER invents a state: unknown backend states
// render verbatim with the neutral style.
function engineBadge(state: EngineState | undefined, provider: string): {
  label: string;
  severity: "good" | "warn" | "bad" | "neutral";
  busy: boolean;
} {
  if (state === "remote") {
    return { label: "Remote endpoint", severity: "good", busy: false };
  }

  switch (state) {
    case "ready":
      return { label: "Engine ready", severity: "good", busy: false };

    case "running":
      return { label: "Engine running", severity: "good", busy: false };

    case "busy":
      return { label: "Inference running", severity: "good", busy: true };

    case "starting":
      return { label: "Engine starting…", severity: "warn", busy: true };

    case "downloading":
      return { label: "Downloading engine…", severity: "warn", busy: true };

    case "stopping":
      return { label: "Engine stopping…", severity: "warn", busy: true };

    case "failed":
      return { label: "Engine failed", severity: "bad", busy: false };

    case "stopped":
      return { label: "Engine stopped", severity: "neutral", busy: false };

    case "idle":
      return {
        label: provider === "remote" ? "Remote provider" : "Engine idle",
        severity: "neutral",
        busy: false,
      };

    default:
      return { label: state ?? "Unknown", severity: "neutral", busy: false };
  }
}

// v1.1.6 §9: user-facing labels for the backend startup phases.
const PHASE_LABELS: Record<string, string> = {
  "downloading-engine": "Downloading engine…",
  "loading-model": "Loading model…",
  "checking-capabilities": "Checking capabilities…",
  "preparing-context": "Preparing context…",
  stopping: "Stopping…",
  stopped: "Engine stopped",
  failed: "Engine failed",
};

// formatBytes renders the estimated-memory figure.
function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) {
    return `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
  }
  if (bytes >= 1024 ** 2) {
    return `${Math.round(bytes / 1024 ** 2)} MiB`;
  }
  return `${bytes} B`;
}

function AgentBody() {
  const models = useRuntimeStore((state) => state.models);
  const sessions = useRuntimeStore((state) => state.sessions);
  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);
  const loading = useRuntimeStore((state) => state.loading);
  const error = useRuntimeStore((state) => state.error);
  const running = useRuntimeStore((state) => state.running);
  const engine = useRuntimeStore((state) => state.engine);

  // v1.1.4Z: the toggle and the badge previously read DIFFERENT sources
  // (models.llamaRunning vs engine.state) and could disagree transiently.
  const engineAlive =
    engine?.state === "ready" ||
    engine?.state === "running" ||
    engine?.state === "busy";
  const pendingAttachments = useRuntimeStore((state) => state.pendingAttachments);
  const attachmentsUploading = useRuntimeStore((state) => state.attachmentsUploading);

  const createSession = useRuntimeStore((state) => state.createSession);
  const deleteSession = useRuntimeStore((state) => state.deleteSession);
  const run = useRuntimeStore((state) => state.run);
  const abort = useRuntimeStore((state) => state.abort);
  const regenerate = useRuntimeStore((state) => state.regenerate);
  const uploadFiles = useRuntimeStore((state) => state.uploadFiles);
  const removePendingAttachment = useRuntimeStore(
    (state) => state.removePendingAttachment,
  );
  const refreshModels = useRuntimeStore((state) => state.refreshModels);
  const startEnginePolling = useRuntimeStore((state) => state.startEnginePolling);
  const stopEnginePolling = useRuntimeStore((state) => state.stopEnginePolling);
  const connectActivity = useRuntimeStore((state) => state.connectActivity);
  const disconnectActivity = useRuntimeStore(
    (state) => state.disconnectActivity,
  );

  const [message, setMessage] = useState("");
  const [config, setConfig] = useState<RuntimeConfig | null>(null);
  const [modelBusy, setModelBusy] = useState(false);
  const [engineBusy, setEngineBusy] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [showActivity, setShowActivity] = useState(false);

  // v1.1.8: Chat / Agent mode surfaces. Both modes share THIS component's
  // session/model/engine wiring — only what is VISIBLE differs. Chat gets
  // a slim model rail and the full-screen picker; Agent keeps the runtime
  // panel, activity, and telemetry.
  const mode = useRuntimeStore((state) => state.mode);
  const chatMode = mode === "chat";

  // Picker visibility: automatic when no usable model exists, overridable
  // by the user (null = follow the automatic state).
  const [pickerOverride, setPickerOverride] = useState<boolean | null>(null);
  const needPicker = (models?.local ?? []).length === 0 || !config?.model;
  const pickerOpen = pickerOverride ?? needPicker;

  useEffect(() => {
    let cancelled = false;

    void initializeAgent()
      .then(async () => {
        const runtimeConfig = await api.config();

        if (cancelled) {
          return;
        }

        setConfig(runtimeConfig);
        connectActivity();
        startEnginePolling();
      })
      .catch(() => {
        // Initialization records the main error in the runtime store.
      });

    return () => {
      cancelled = true;
      disconnectActivity();
      // v1.1.4Z: stop the /api/engine poll on unmount — it previously ran
      // for the whole app lifetime once this view had been opened once.
      stopEnginePolling();
    };
  }, [connectActivity, disconnectActivity, startEnginePolling, stopEnginePolling]);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );

  const localModels = models?.local ?? [];

  const badge = engineBadge(engine?.state, engine?.provider ?? "local");

  // v1.1.6 §10: the card facts of the model that will serve (selection
  // order: chosen config model → engine-served model).
  const selectedModelCard = useMemo(() => {
    const wanted = config?.model || engine?.model || activeSession?.model;
    if (!wanted) {
      return null;
    }
    return (
      localModels.find((m) => m.id === wanted) ??
      localModels.find((m) => wanted.startsWith(m.id)) ??
      null
    );
  }, [localModels, config?.model, engine?.model, activeSession?.model]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const value = message.trim();

    if (!value || running) {
      return;
    }

    setMessage("");

    try {
      await run(value);
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleNewSession() {
    try {
      await createSession();
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleDeleteSession() {
    if (!activeSessionId) {
      return;
    }

    try {
      await deleteSession(activeSessionId);
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleRegenerate() {
    try {
      await regenerate();
    } catch {
      // Store exposes the runtime error.
    }
  }

  // v1.1.8: returns success so the model picker can close itself only
  // when the switch actually applied.
  async function switchModel(nextModel: string): Promise<boolean> {
    if (!nextModel || nextModel === config?.model || modelBusy) {
      return false;
    }

    setModelBusy(true);
    setModelError(null);

    try {
      const nextConfig = await api.updateConfig({
        model: nextModel,
      });

      setConfig(nextConfig);

      if (engineAlive) {
        await api.llama("stop");
      }

      await api.llama("start");
      await refreshModels();

      return true;
    } catch (switchError) {
      setModelError(
        switchError instanceof Error
          ? switchError.message
          : "Unable to switch model.",
      );

      return false;
    } finally {
      setModelBusy(false);
    }
  }

  async function toggleEngine() {
    if (engineBusy) {
      return;
    }

    setEngineBusy(true);
    setModelError(null);

    try {
      await api.llama(engineAlive ? "stop" : "start");
      await refreshModels();
    } catch (engineError) {
      setModelError(
        engineError instanceof Error
          ? engineError.message
          : "Unable to control the local engine.",
      );
    } finally {
      setEngineBusy(false);
    }
  }

  function handleFileSelection(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files ?? []);

    event.target.value = "";

    if (files.length === 0 || !activeSessionId) {
      return;
    }

    void uploadFiles(files).catch(() => {
      // Store exposes the upload error.
    });
  }

  const onPickFiles = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  useEffect(() => {
    function handleNewSessionRequest() {
      void handleNewSession();
    }

    window.addEventListener("sheytan:new-session", handleNewSessionRequest);

    return () => {
      window.removeEventListener(
        "sheytan:new-session",
        handleNewSessionRequest,
      );
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [createSession]);

  const canAttach = Boolean(activeSessionId) && !attachmentsUploading;

  return (
    <>
      <section
        className={`workspace-content${chatMode ? " mode-chat" : ""}`}
      >
        {chatMode && (
          <div className="chat-rail">
            <span
              className={`status-dot severity-${badge.severity} ${
                badge.busy ? "pulsing" : ""
              }`}
            />

            <span className="chat-rail-state">{badge.label}</span>

            <select
              className="runtime-select chat-rail-select"
              value={config?.model ?? ""}
              onChange={(event) => void switchModel(event.target.value)}
              disabled={
                modelBusy ||
                config?.provider !== "local" ||
                localModels.length === 0
              }
              aria-label="Model"
            >
              <option value="">
                {localModels.length === 0
                  ? "No local GGUF models found"
                  : "Select local model"}
              </option>

              {localModels.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.name}
                  {model.quantization ? ` · ${model.quantization}` : ""}
                  {model.parameterInfo ? ` · ${model.parameterInfo}` : ""}
                </option>
              ))}
            </select>

            <button
              type="button"
              className="text-button"
              onClick={() => setPickerOverride(!pickerOpen)}
            >
              {pickerOpen ? "Hide models" : "All models"}
            </button>
          </div>
        )}

        {/* v1.1.8: the model picker owns the content area whenever it is
            open (automatic while no usable model exists). Otherwise the
            conversation stream fills the same space. */}
        {pickerOpen ? (
          <ModelPicker
            activeModel={
              config?.model ?? engine?.model ?? activeSession?.model ?? null
            }
            busy={modelBusy}
            onUse={(id) => {
              void switchModel(id).then((applied) => {
                if (applied) {
                  setPickerOverride(false);
                }
              });
            }}
            onClose={() => setPickerOverride(false)}
          />
        ) : (
          <MessageStream />
        )}

        {!chatMode && (
        <aside className="runtime-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">RUNTIME</span>
              <strong>System</strong>
            </div>
          </div>

          <div className="runtime-section">
            <span className="eyebrow">ENGINE</span>

            <div className="engine-status">
              <span
                className={`status-dot severity-${badge.severity} ${
                  badge.busy ? "pulsing" : ""
                }`}
              />

              <div>
                <strong>{badge.label}</strong>

                <span>
                  {engine?.model
                    ? engine.model
                    : engine?.provider === "remote"
                      ? "Remote endpoint"
                      : "Local llama.cpp"}
                </span>
              </div>
            </div>

            {/* v1.1.6 §9: real startup progress states while the engine
                prepares — the UI stays usable during the whole flow. */}
            {engine?.phase && engine.phase !== "ready" && engine.phase !== "waiting" && (
              <span className="runtime-hint engine-phase">
                {PHASE_LABELS[engine.phase] ?? engine.phase}
              </span>
            )}

            {/* v1.1.6 §11: never hide degraded startup behind a green
                status — if "ready" could not be verified, say so. */}
            {engine?.degraded && (
              <span className="runtime-hint engine-phase engine-phase-warn">
                Degraded startup: the serving model could not be verified
                {engine.detail ? ` — ${engine.detail}` : ""}
              </span>
            )}

            {engine?.detail ? (
              <span className="runtime-hint engine-detail" title={engine.detail}>
                {engine.detail}
              </span>
            ) : null}

            <div className="header-actions">
              <button
                type="button"
                className="secondary-button"
                onClick={() => void toggleEngine()}
                disabled={engineBusy}
              >
                {engineBusy
                  ? "Working…"
                  : engineAlive
                    ? "Stop engine"
                    : "Start engine"}
              </button>

              <button
                type="button"
                className="text-button"
                onClick={() => setShowActivity((value) => !value)}
              >
                {showActivity ? "Hide activity" : "Show activity"}
              </button>
            </div>
          </div>

          {/* v1.1.6 §10 first use: when no valid model exists, give the
              user a clear path instead of a dead dropdown. v1.1.8: the
              button opens the real model picker (no DOM query hack). */}
          {localModels.length === 0 && !config?.model && (
            <div className="runtime-section first-use">
              <span className="eyebrow">GET STARTED</span>

              <strong className="first-use-title">No model selected</strong>

              <span className="runtime-hint">
                Place a GGUF model in the models folder, then choose it
                here. Path: <code>{config?.modelsDir || "models/"}</code>
              </span>

              <div className="header-actions">
                <button
                  type="button"
                  className="secondary-button"
                  onClick={() => setPickerOverride(true)}
                >
                  Choose model
                </button>

                <button
                  type="button"
                  className="text-button"
                  onClick={() => {
                    void api.openModelsFolder().catch(() => {
                      /* best effort; the path is shown above */
                    });
                  }}
                >
                  Open models folder
                </button>
              </div>
            </div>
          )}

          <div className="runtime-section">
            <span className="eyebrow">MODEL</span>

            <select
              className="runtime-select"
              value={config?.model ?? ""}
              onChange={(event) => void switchModel(event.target.value)}
              disabled={
                modelBusy ||
                config?.provider !== "local" ||
                localModels.length === 0
              }
            >
              <option value="">
                {localModels.length === 0
                  ? "No local GGUF models found"
                  : "Select local model"}
              </option>

              {localModels.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.name}
                  {model.quantization ? ` · ${model.quantization}` : ""}
                  {model.parameterInfo ? ` · ${model.parameterInfo}` : ""}
                </option>
              ))}
            </select>

            <div className="session-detail">
              <span>ACTIVE</span>

              <strong>
                {engine?.model ||
                  config?.model ||
                  activeSession?.model ||
                  "No model selected"}
              </strong>
            </div>

            {/* v1.1.6 §10: the model card facts — backend-measured, no
                engine internals during normal startup. */}
            {selectedModelCard && (
              <div className="model-card-facts">
                {selectedModelCard.architecture && (
                  <div className="session-detail">
                    <span>Architecture</span>
                    <strong>{selectedModelCard.architecture}</strong>
                  </div>
                )}
                {selectedModelCard.quantization && (
                  <div className="session-detail">
                    <span>Quantization</span>
                    <strong>{selectedModelCard.quantization}</strong>
                  </div>
                )}
                {!!selectedModelCard.contextLength && (
                  <div className="session-detail">
                    <span>Context maximum</span>
                    <strong>{selectedModelCard.contextLength.toLocaleString()} tok</strong>
                  </div>
                )}
                {!!selectedModelCard.estimatedMemoryBytes && (
                  <div className="session-detail">
                    <span>Estimated memory</span>
                    <strong>{formatBytes(selectedModelCard.estimatedMemoryBytes)}</strong>
                  </div>
                )}
                <div className="session-detail">
                  <span>Backend</span>
                  <strong>{engine?.backend ?? "llama"}</strong>
                </div>
                <div className="session-detail">
                  <span>Status</span>
                  <strong>{badge.label}</strong>
                </div>
              </div>
            )}

            {modelBusy ? (
              <span className="runtime-hint">Switching model…</span>
            ) : null}
          </div>

          <div className="runtime-section">
            <span className="eyebrow">SESSION</span>

            <div className="session-detail">
              <span>ID</span>
              <strong>
                {activeSessionId ? activeSessionId.slice(0, 16) : "None"}
              </strong>
            </div>

            <div className="header-actions">
              <button
                type="button"
                className="secondary-button"
                onClick={() => void handleNewSession()}
              >
                New session
              </button>

              <button
                type="button"
                className="secondary-button"
                onClick={() => void handleDeleteSession()}
                disabled={!activeSessionId}
              >
                Delete session
              </button>
            </div>
          </div>

          {/* v1.1.8: compact live telemetry in the Agent surface — same
              /api/perf source as the diagnostic HUD, never fabricated. */}
          <PerfStrip />

          {showActivity ? <ActivityStream /> : null}
        </aside>
        )}
      </section>

      <form className="composer" onSubmit={handleSubmit}>
        {pendingAttachments.length > 0 || attachmentsUploading ? (
          <div className="composer-attachments">
            {attachmentsUploading ? (
              <span className="runtime-hint">Staging attachments…</span>
            ) : null}

            {pendingAttachments.map((attachment) => (
              <AttachmentChip
                key={attachment.id}
                attachment={attachment}
                onRemove={(id) => void removePendingAttachment(id)}
              />
            ))}
          </div>
        ) : null}

        <div className="composer-shell">
          <input
            ref={fileInputRef}
            type="file"
            multiple
            hidden
            onChange={handleFileSelection}
            aria-label="Attach files"
          />

          <textarea
            value={message}
            onChange={(event) => setMessage(event.target.value)}
            placeholder={
              !activeSessionId
                ? "Create a session to begin..."
                : chatMode
                  ? "Message SHEYTAN..."
                  : "Describe what SHEYTAN should forge..."
            }
            disabled={!activeSessionId || running}
            rows={3}
            onKeyDown={(event) => {
              if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
          />

          <div className="composer-footer">
            <div className="composer-footer-left">
              <button
                type="button"
                className="text-button"
                onClick={onPickFiles}
                disabled={!canAttach}
                title="Attach files (text, code, images)"
              >
                {attachmentsUploading ? "Staging…" : "＋ Attach"}
              </button>

              <span>
                {activeSessionId
                  ? "Ctrl/Cmd + Enter to run"
                  : "Create a session first"}
              </span>
            </div>

            <div className="composer-footer-actions">
              {!running && activeSessionId ? (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void handleRegenerate()}
                  disabled={running}
                  title="Re-run the last user message"
                >
                  Regenerate
                </button>
              ) : null}

              {running ? (
                <button
                  type="button"
                  className="stop-button"
                  onClick={() => void abort()}
                >
                  Stop
                </button>
              ) : (
                <button
                  type="submit"
                  className="send-button"
                  disabled={!activeSessionId || !message.trim() || loading}
                >
                  {chatMode ? "Send" : "Forge →"}
                </button>
              )}
            </div>
          </div>
        </div>
      </form>

      {modelError ? (
        <div className="error-banner" role="alert">
          <span>{modelError}</span>
        </div>
      ) : null}

      {error ? (
        <div className="error-banner" role="alert">
          <span>{error}</span>
        </div>
      ) : null}
    </>
  );
}

export default AgentBody;
