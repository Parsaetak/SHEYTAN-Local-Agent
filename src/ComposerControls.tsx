// ComposerControls.tsx — v1.2.5 per-request composer controls.
//
// Two REAL controls (not visual-only):
//
//   Thinking ▾    auto | fast | thinking — sent with every run request;
//                 changes the backend's tier posture, the thinking nudge
//                 and the verification depth (see agent.WithThinkingMode).
//
//   Tools ▾       AUTO | MANUAL + per-tool checkboxes — in MANUAL only
//                 the selected tools are offered and executable; the
//                 restriction is enforced server-side and is never
//                 silently re-enabled (see agent.WithToolPolicy).
//
// The live status chip (backend "status" events: measured tier/token
// telemetry) rides beside them so the user sees what the controls did.

import { useMemo, useState } from "react";

import { useRuntimeStore } from "./store";
import { THINKING_OPTIONS, type ThinkingControl } from "./run-events";

function ComposerControls() {
  const thinkingControl = useRuntimeStore((s) => s.thinkingControl);
  const setThinkingControl = useRuntimeStore((s) => s.setThinkingControl);
  const toolPolicyMode = useRuntimeStore((s) => s.toolPolicyMode);
  const setToolPolicyMode = useRuntimeStore((s) => s.setToolPolicyMode);
  const toolAllowlist = useRuntimeStore((s) => s.toolAllowlist);
  const setToolAllowed = useRuntimeStore((s) => s.setToolAllowed);
  const tools = useRuntimeStore((s) => s.tools);
  const liveStatus = useRuntimeStore((s) => s.liveStatus);

  const [thinkingOpen, setThinkingOpen] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);

  const thinkingLabel = useMemo(() => {
    const found = THINKING_OPTIONS.find((o) => o.value === thinkingControl);
    return found ? found.label : "Auto";
  }, [thinkingControl]);

  const allowedCount = toolAllowlist.length;

  return (
    <div className="composer-controls" role="group" aria-label="Request controls">
      {/* Thinking control */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${thinkingControl !== "auto" ? " active" : ""}`}
          aria-haspopup="listbox"
          aria-expanded={thinkingOpen}
          onClick={() => {
            setThinkingOpen((v) => !v);
            setToolsOpen(false);
          }}
          title="Controls how much reasoning and context the agent uses for this request"
        >
          Thinking · {thinkingLabel} ▾
        </button>

        {thinkingOpen ? (
          <div className="control-menu-panel" role="listbox">
            {THINKING_OPTIONS.map((option) => (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={thinkingControl === option.value}
                className={`control-option${
                  thinkingControl === option.value ? " selected" : ""
                }`}
                onClick={() => {
                  setThinkingControl(option.value as ThinkingControl);
                  setThinkingOpen(false);
                }}
              >
                <span className="control-option-label">{option.label}</span>
                <span className="control-option-hint">{option.hint}</span>
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {/* Tool policy control */}
      <div className="control-menu">
        <button
          type="button"
          className={`control-button${toolPolicyMode === "manual" ? " active" : ""}`}
          aria-haspopup="listbox"
          aria-expanded={toolsOpen}
          onClick={() => {
            setToolsOpen((v) => !v);
            setThinkingOpen(false);
          }}
          title="AUTO lets the agent choose tools; MANUAL restricts execution to your selection"
        >
          Tools · {toolPolicyMode === "manual" ? `Manual (${allowedCount})` : "Auto"} ▾
        </button>

        {toolsOpen ? (
          <div className="control-menu-panel control-tools-panel" role="listbox">
            <div className="control-policy-row">
              <button
                type="button"
                className={`control-policy${toolPolicyMode === "auto" ? " selected" : ""}`}
                onClick={() => setToolPolicyMode("auto")}
              >
                AUTO
              </button>
              <button
                type="button"
                className={`control-policy${toolPolicyMode === "manual" ? " selected" : ""}`}
                onClick={() => setToolPolicyMode("manual")}
              >
                MANUAL
              </button>
            </div>

            {toolPolicyMode === "manual" ? (
              <div className="control-tool-list">
                {tools.length === 0 ? (
                  <span className="runtime-hint">No tools registered</span>
                ) : (
                  tools.map((tool) => {
                    const checked = toolAllowlist.some(
                      (n) => n.toLowerCase() === tool.name.toLowerCase(),
                    );

                    return (
                      <label key={tool.name} className="control-tool-row">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={(e) =>
                            setToolAllowed(tool.name, e.target.checked)
                          }
                        />
                        <span className="control-tool-name">{tool.name}</span>
                        {tool.description ? (
                          <span className="control-tool-desc">
                            {tool.description}
                          </span>
                        ) : null}
                      </label>
                    );
                  })
                )}

                {allowedCount === 0 ? (
                  <span className="runtime-hint control-tools-hint">
                    Nothing selected — manual mode allows no tools (pure chat).
                  </span>
                ) : null}
              </div>
            ) : (
              <span className="control-option-hint control-tools-hint">
                The agent selects the tools each task needs; restrictions can
                be imposed per request below.
              </span>
            )}
          </div>
        ) : null}
      </div>

      {/* Live status chip — backend "status" events only (measured values) */}
      {liveStatus ? (
        <span className="control-status-chip" role="status">
          {liveStatus}
        </span>
      ) : null}
    </div>
  );
}

export default ComposerControls;
