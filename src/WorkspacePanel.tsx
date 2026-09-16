import { useCallback, useEffect, useMemo, useState } from "react";

import {
  api,
  type WorkspaceSummary as WorkspaceSummaryData,
} from "./api";
import { useRuntimeStore } from "./store";

// v1.2.4: the Workspace panel — the whole work environment in one glance:
// Project → files → active task → model → runtime → current state, plus
// the quick actions. Technical controls (change root, terminal) stay
// secondary; the state summary and the common actions lead.

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatWhen(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  return t.toLocaleString();
}

function WorkspaceLoading() {
  return (
    <div className="workspace-panel">
      <div className="wb-summary-card wb-card">
        <div className="wb-summary-line">
          <span className="wb-chip skeleton">loading…</span>
          <span className="wb-chip skeleton">loading…</span>
        </div>
      </div>
    </div>
  );
}

function WorkspacePanel() {
  const [summary, setSummary] = useState<WorkspaceSummaryData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionNote, setActionNote] = useState<string | null>(null);
  const [pickPath, setPickPath] = useState("");
  const [showSwitch, setShowSwitch] = useState(false);

  const running = useRuntimeStore((s) => s.running);
  const activeSessionId = useRuntimeStore((s) => s.activeSessionId);
  const createSession = useRuntimeStore((s) => s.createSession);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.workspace();
      setSummary(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workspace.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Refresh the summary when a run settles — the agent state chip and the
  // recent-file list may both change as a side effect of the run.
  useEffect(() => {
    if (!loading) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running, activeSessionId]);

  async function reveal(target: "files" | "terminal") {
    setActionNote(null);
    try {
      await api.workspaceReveal(target);
      setActionNote(
        target === "files"
          ? "Opened the folder in your file manager."
          : "Opened a terminal at the workspace folder.",
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reveal failed.");
    }
  }

  async function switchRoot(path: string) {
    setError(null);
    setActionNote(null);
    try {
      const result = await api.workspaceSwitch(path);
      setSummary(result.summary);
      setActionNote(
        `Workspace changed → ${result.summary.root}${
          result.changed && result.changed.length > 0
            ? ` (${result.changed.join(", ")})`
            : ""
        }`,
      );
      setShowSwitch(false);
      setPickPath("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Workspace switch failed.");
    }
  }

  const summaryLine = useMemo(() => {
    if (!summary) return [];
    const chips: { label: string; value: string }[] = [
      {
        label: "Project",
        value: summary.root.split(/[\\/]/).filter(Boolean).pop() ?? summary.root,
      },
      {
        label: "Files",
        value: summary.recentFiles?.length
          ? `${summary.recentFiles.length} recent`
          : summary.exists
            ? "—"
            : "missing",
      },
      {
        label: "Task",
        value: summary.activeSession?.title
          ? summary.activeSession.title
          : summary.activeSession
            ? `session ${summary.activeSession.id}`
            : "no session",
      },
      {
        label: "Model",
        value: summary.runtime?.model || "none",
      },
      {
        label: "Runtime",
        value: summary.runtime?.engineState || "unknown",
      },
      {
        label: "State",
        value: summary.agent?.running ? "running" : "idle",
      },
    ];
    return chips;
  }, [summary]);

  if (loading && !summary) {
    return <WorkspaceLoading />;
  }

  return (
    <div className="workspace-panel">
      {error ? (
        <div className="error-banner" role="alert">
          <span>{error}</span>
          <button type="button" className="btn btn-sm" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      ) : null}

      {actionNote ? (
        <div className="wb-action-note" role="status">
          {actionNote}
        </div>
      ) : null}

      {/* Project → files → task → model → runtime → state */}
      <div className="wb-card wb-summary-card">
        <div className="wb-card-title">Current environment</div>
        <div className="wb-summary-line">
          {summaryLine.map((chip) => (
            <span className="wb-chip" key={chip.label} title={`${chip.label}: ${chip.value}`}>
              <span className="wb-chip-label">{chip.label}</span>
              <span className="wb-chip-value">{chip.value}</span>
            </span>
          ))}
        </div>
        <div className="wb-summary-path" title={summary?.root}>
          {summary?.root}
        </div>
      </div>

      {/* Quick actions */}
      <div className="wb-card">
        <div className="wb-card-title">Quick actions</div>
        <div className="wb-actions">
          <button
            type="button"
            className="btn"
            onClick={() => void reveal("files")}
            disabled={!summary?.exists}
            title="Open this folder in your file manager"
          >
            Open in Explorer
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => void reveal("terminal")}
            disabled={!summary?.exists}
            title="Open a terminal here"
          >
            Open terminal
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => {
              createSession();
              void refresh();
            }}
            title="Start a fresh session"
          >
            New session
          </button>
          <button type="button" className="btn" onClick={() => void refresh()}>
            Refresh
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => setShowSwitch((v) => !v)}
            title="Point SHEYTAN at a different project folder"
          >
            Change workspace
          </button>
        </div>

        {showSwitch ? (
          <form
            className="wb-switch-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (pickPath.trim()) {
                void switchRoot(pickPath.trim());
              }
            }}
          >
            <label htmlFor="wb-path-input">Project folder path</label>
            <input
              id="wb-path-input"
              type="text"
              value={pickPath}
              placeholder="e.g. C:\dev\my-project or /home/me/my-project"
              onChange={(event) => setPickPath(event.target.value)}
              autoFocus
            />
            <div className="wb-switch-actions">
              <button
                type="submit"
                className="btn btn-primary"
                disabled={!pickPath.trim()}
              >
                Switch
              </button>
              <button
                type="button"
                className="btn"
                onClick={() => setShowSwitch(false)}
              >
                Cancel
              </button>
            </div>
          </form>
        ) : null}

        {summary?.recentWorkspaces && summary.recentWorkspaces.length > 0 ? (
          <div className="wb-recents">
            <span className="wb-muted">Recent:</span>
            {summary.recentWorkspaces.map((recent) => (
              <button
                type="button"
                key={recent}
                className="wb-chip wb-chip-btn"
                title={recent}
                onClick={() => void switchRoot(recent)}
              >
                {recent.split(/[\\/]/).filter(Boolean).pop() ?? recent}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {/* Project health */}
      <div className="wb-grid">
        <div className="wb-card">
          <div className="wb-card-title">Project health</div>
          {summary?.project && Object.keys(summary.project).length > 1 ? (
            <dl className="wb-facts">
              {summary.project.languages && summary.project.languages.length > 0 ? (
                <div className="wb-fact">
                  <dt>Languages</dt>
                  <dd>{summary.project.languages.join(", ")}</dd>
                </div>
              ) : null}
              {summary.project.buildCmd ? (
                <div className="wb-fact">
                  <dt>Build</dt>
                  <dd>
                    <code>{summary.project.buildCmd}</code>
                    {summary.project.buildVerifiedAt ? (
                      <span className="wb-verified"> verified</span>
                    ) : null}
                  </dd>
                </div>
              ) : null}
              {summary.project.testCmd ? (
                <div className="wb-fact">
                  <dt>Test</dt>
                  <dd>
                    <code>{summary.project.testCmd}</code>
                    {summary.project.testVerifiedAt ? (
                      <span className="wb-verified"> verified</span>
                    ) : null}
                  </dd>
                </div>
              ) : null}
              {summary.project.lessons && summary.project.lessons.length > 0 ? (
                <div className="wb-fact">
                  <dt>Lessons</dt>
                  <dd>{summary.project.lessons.length} recorded</dd>
                </div>
              ) : null}
              {summary.project.conventions && summary.project.conventions.length > 0 ? (
                <div className="wb-fact">
                  <dt>Conventions</dt>
                  <dd>{summary.project.conventions.length} recorded</dd>
                </div>
              ) : null}
              {summary.project.lastObservedAt ? (
                <div className="wb-fact">
                  <dt>Observed</dt>
                  <dd>{formatWhen(summary.project.lastObservedAt)}</dd>
                </div>
              ) : null}
            </dl>
          ) : (
            <p className="wb-muted">
              No measured facts yet for this folder. Run a task or a Coding Lab
              job here and SHEYTAN will record languages, build/test commands
              and lessons automatically.
            </p>
          )}
        </div>

        {/* Recent files */}
        <div className="wb-card">
          <div className="wb-card-title">Recent files</div>
          {summary?.recentFiles && summary.recentFiles.length > 0 ? (
            <ul className="wb-files">
              {summary.recentFiles.map((file) => (
                <li key={file.name} title={file.name}>
                  <span className="wb-file-name">{file.name}</span>
                  <span className="wb-file-meta">
                    {formatBytes(file.size)} · {formatWhen(file.modTime)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="wb-muted">
              No files in this workspace yet.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

export default WorkspacePanel;
