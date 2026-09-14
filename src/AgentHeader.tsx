import { memo } from "react";

import { useRuntimeStore } from "./store";

// formatTok renders the compact token figure the header shows:
// 15.2K / 16K.
function formatTok(tokens: number): string {
  if (tokens >= 1024) {
    const k = tokens / 1024;
    return `${k >= 10 ? Math.round(k) : Math.round(k * 10) / 10}K`;
  }
  return String(tokens);
}

// v1.1.8: the top-level Chat / Agent segmented switch. Exactly one mode
// is active; the mode decides which controls each surface exposes while
// both keep using the same session and engine runtime underneath.
const ModeSwitch = memo(function ModeSwitch() {
  const mode = useRuntimeStore((state) => state.mode);
  const setMode = useRuntimeStore((state) => state.setMode);

  return (
    <div className="mode-switch" role="tablist" aria-label="Workspace mode">
      <button
        type="button"
        role="tab"
        aria-selected={mode === "chat"}
        className={`mode-switch-option${mode === "chat" ? " active" : ""}`}
        onClick={() => setMode("chat")}
      >
        Chat
      </button>

      <button
        type="button"
        role="tab"
        aria-selected={mode === "agent"}
        className={`mode-switch-option${mode === "agent" ? " active" : ""}`}
        onClick={() => setMode("agent")}
      >
        Agent
      </button>
    </div>
  );
});

const AgentHeader = memo(function AgentHeader() {
  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);

  const sessions = useRuntimeStore((state) => state.sessions);

  const running = useRuntimeStore((state) => state.running);

  const mode = useRuntimeStore((state) => state.mode);

  const sessionContext = useRuntimeStore((state) => state.sessionContext);
  const sessionContextError = useRuntimeStore((state) => state.sessionContextError);
  const setSessionContext = useRuntimeStore((state) => state.setSessionContext);

  const engine = useRuntimeStore((state) => state.engine);

  const activeSession =
    sessions.find((session) => session.id === activeSessionId) ?? null;

  // v1.1.8: Chat shows plain conversation wording; Agent keeps the
  // engineering voice. Same session, different framing.
  const defaultTitle = mode === "chat" ? "New chat" : "Forge a new task";

  const options = sessionContext?.options ?? [];

  const usage = sessionContext
    ? `${formatTok(sessionContext.used)} / ${formatTok(sessionContext.effective)}`
    : null;
  const pressurePct = sessionContext
    ? Math.round(sessionContext.pressure * 100)
    : null;

  // The selector reflects THIS chat's policy; when the policy was
  // clamped (engine window smaller than requested), the effective value
  // is what actually serves — shown as the fallback option.
  const chosen = sessionContext?.sessionPolicy || sessionContext?.effective || 0;
  const chosenInOptions = options.some((o) => o.tokens === chosen);

  // v1.1.8: context engineering controls are Agent-surface tools. Chat
  // stays minimal (model, conversation, input, send/stop, attachments).
  const showContextControls = mode === "agent";

  return (
    <>
      <h1>{activeSession?.title || defaultTitle}</h1>

      <div className="header-actions">
        <ModeSwitch />

        {showContextControls && sessionContext && (
          <label className="ctx-pill" title="Context window for THIS chat only">
            <span className="ctx-pill-label">CONTEXT</span>
            <select
              className="ctx-select"
              value={chosenInOptions ? chosen : ""}
              onChange={(event) => {
                const tokens = Number(event.target.value);
                if (tokens > 0) void setSessionContext(tokens);
              }}
              aria-label="Context window"
            >
              {!chosenInOptions && (
                <option value="">{formatTok(sessionContext.effective)}</option>
              )}
              {options.map((option) => (
                <option
                  key={option.tokens}
                  value={option.tokens}
                  disabled={!option.available}
                  title={option.reason}
                >
                  {option.label}
                  {option.engineClamped ? " ⌄" : ""}
                  {option.classification === "caution" ? " (!)" : ""}
                </option>
              ))}
            </select>
          </label>
        )}

        {showContextControls && usage && (
          <span
            className={`ctx-pill ctx-usage${
              pressurePct !== null && pressurePct >= 90 ? " ctx-critical" : ""
            }`}
            title={
              sessionContext?.resourceReason ??
              `Effective ${sessionContext?.effective} tok · input budget ${sessionContext?.usableInput} · output reserve ${sessionContext?.outputReserve}`
            }
          >
            <span className="ctx-pill-label">USED</span>
            <span className="ctx-usage-value">
              {usage}
              {pressurePct !== null ? ` · ${pressurePct}%` : ""}
            </span>
          </span>
        )}

        {showContextControls && sessionContextError && (
          <span className="ctx-pill ctx-usage ctx-critical" title={sessionContextError}>
            <span className="ctx-pill-label">CONTEXT</span>
            <span className="ctx-usage-value">unavailable</span>
          </span>
        )}

        {/* v1.1.6 §11: the status pill reflects the backend phase and the
            verified-readiness proof, not a local guess. Kept in BOTH
            modes — one glance answers "is the engine ready?". */}
        <span
          className={`runtime-pill${
            engine?.degraded ? " runtime-pill-degraded" : ""
          }`}
          title={
            engine?.degraded
              ? "Engine reports ready but the serving model could not be verified — degraded startup"
              : engine?.verifiedModel
                ? `Verified: ${engine.verifiedModel}`
                : undefined
          }
        >
          {running
            ? "RUNNING"
            : engine?.phase === "ready"
              ? "READY"
              : (engine?.phase ?? "READY").toUpperCase().replace(/-/g, " ")}
        </span>
      </div>
    </>
  );
});

export default AgentHeader;
