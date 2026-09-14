import { memo } from "react";

import type { Session } from "./api";
import { useRuntimeStore } from "./store";

const SessionItem = memo(function SessionItem({
  session,
  active,
  onSelect,
}: {
  session: Session;
  active: boolean;
  onSelect: (id: string) => void;
}) {
  return (
    <button
      type="button"
      className={`session-item ${active ? "active" : ""}`}
      onClick={() => onSelect(session.id)}
    >
      <span className="session-icon">◈</span>

      <span className="session-copy">
        <strong>{session.title || "Untitled session"}</strong>

        <span>{session.id.slice(0, 8)}</span>
      </span>
    </button>
  );
});

const AgentSidebar = memo(function AgentSidebar() {
  const sessions = useRuntimeStore((state) => state.sessions);

  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);

  const loading = useRuntimeStore((state) => state.loading);

  const selectSession = useRuntimeStore((state) => state.selectSession);

  // v1.1.9: the sidebar speaks in the voice of the active mode — same
  // sessions underneath, different framing.
  const mode = useRuntimeStore((state) => state.mode);

  function requestNewSession() {
    window.dispatchEvent(new CustomEvent("sheytan:new-session"));
  }

  return (
    <>
      <div className="sidebar-heading sidebar-heading-secondary">
        <div>
          <span className="eyebrow">{mode === "chat" ? "CHAT" : "AGENT"}</span>

          <strong>Sessions</strong>
        </div>

        <button
          type="button"
          className="icon-button"
          onClick={requestNewSession}
          title="New session"
          aria-label="New session"
        >
          +
        </button>
      </div>

      <div className="session-list">
        {sessions.length === 0 && !loading ? (
          <div className="empty-sidebar">No sessions yet.</div>
        ) : (
          sessions.map((session) => (
            <SessionItem
              key={session.id}
              session={session}
              active={session.id === activeSessionId}
              onSelect={selectSession}
            />
          ))
        )}
      </div>
    </>
  );
});

export default AgentSidebar;
