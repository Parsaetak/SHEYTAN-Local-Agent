import { Suspense, lazy, useEffect, useState } from "react";
import type { CSSProperties } from "react";

import {
  getWorkspaceHref,
  getWorkspaceLayer,
  parseWorkspaceHash,
  rememberView,
  restoreView,
  visibleWorkspaceLayers,
  type WorkspaceView,
} from "./workspace";
import { installShortcuts } from "./shortcuts";
import { SHORTCUTS } from "./shortcuts";
import { PanelErrorBoundary } from "./ErrorBoundary";
import { useRuntimeStore } from "./store";

const AgentBody = lazy(() => import("./AgentBody"));
const AgentHeader = lazy(() => import("./AgentHeader"));
const AgentSidebar = lazy(() => import("./AgentSidebar"));
const LabPanel = lazy(() => import("./LabPanel"));
const ResearchPanel = lazy(() => import("./ResearchPanel"));
const SettingsPanel = lazy(() => import("./SettingsPanel"));
const SystemPanel = lazy(() => import("./SystemPanel"));
const WorkspacePanel = lazy(() => import("./WorkspacePanel"));

function PanelLoading({ label }: { label: string }) {
  return (
    <div className="panel-loading">
      <div className="panel-loading-mark">✦</div>
      <strong>Loading {label}</strong>
      <span>Initializing this workspace layer…</span>
    </div>
  );
}

function SidebarLayerLoading() {
  return (
    <div className="panel-loading">
      <div className="panel-loading-mark">✦</div>
      <strong>Loading Agent</strong>
      <span>Initializing session layer…</span>
    </div>
  );
}

// v1.2.4: the "?" shortcut help overlay — a plain card, no heavy
// animation, closed by Escape or the close button.
function ShortcutHelpOverlay({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="shortcuts-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <div className="shortcuts-card">
        <div className="shortcuts-card-head">
          <strong>Keyboard shortcuts</strong>
          <button type="button" className="btn btn-sm" onClick={onClose}>
            Close
          </button>
        </div>
        <dl className="shortcuts-list">
          {SHORTCUTS.map((shortcut) => (
            <div className="shortcuts-row" key={shortcut.id}>
              <dt>{shortcut.description}</dt>
              <dd>
                <kbd>{shortcut.keys}</kbd>
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}

function App() {
  const appVersion = useRuntimeStore((state) => state.app?.appVersion ?? null);
  const connection = useRuntimeStore((state) => state.connection);
  // v1.1.9: the navigation is mode-aware — Chat hides Agent machinery
  // (Coding Lab) so the primary surface stays minimal.
  const mode = useRuntimeStore((state) => state.mode);

  const [view, setView] = useState<WorkspaceView>(() => parseWorkspaceHash());
  const [helpOpen, setHelpOpen] = useState(false);

  // v1.2.4: restore the last visited layer on restart when the URL carries
  // no explicit hash (deep links still win).
  useEffect(() => {
    if (!window.location.hash) {
      const stored = restoreView();
      if (stored && stored !== "agent") {
        window.history.replaceState(null, "", getWorkspaceHref(stored));
        setView(stored);
      }
    }
  }, []);

  useEffect(() => {
    function syncViewFromLocation() {
      setView(parseWorkspaceHash());
    }

    window.addEventListener("hashchange", syncViewFromLocation);
    window.addEventListener("popstate", syncViewFromLocation);

    const normalizedView = parseWorkspaceHash();
    const normalizedHref = getWorkspaceHref(normalizedView);

    if (window.location.href !== normalizedHref) {
      window.history.replaceState(null, "", normalizedHref);
      setView(normalizedView);
    }

    return () => {
      window.removeEventListener("hashchange", syncViewFromLocation);
      window.removeEventListener("popstate", syncViewFromLocation);
    };
  }, []);

  function changeView(nextView: WorkspaceView) {
    if (nextView === view) {
      return;
    }

    window.history.pushState(null, "", getWorkspaceHref(nextView));
    setView(nextView);
    rememberView(nextView);
  }

  const layer = getWorkspaceLayer(view);

  // v1.1.9: if the restored view belongs to machinery the current mode
  // hides (e.g. Coding Lab while in Chat), fall back to the workspace.
  const visibleLayers = visibleWorkspaceLayers(mode);
  const viewVisible = visibleLayers.some((candidate) => candidate.id === view);
  const activeLayer = viewVisible
    ? layer
    : (visibleLayers[0] ?? getWorkspaceLayer("agent"));
  const effectiveView = viewVisible ? view : activeLayer.id;

  // v1.1.6 §13: a consistent SHEYTAN identity on every workspace layer
  // (window title / taskbar). "SHEYTAN", "SHEYTAN — Settings", … — never
  // generic shell or localhost strings.
  useEffect(() => {
    document.title =
      effectiveView === "agent" ? "SHEYTAN" : `SHEYTAN — ${activeLayer.label}`;
  }, [effectiveView, activeLayer.label]);

  // v1.2.4: global keyboard shortcuts (single listener, registered once).
  useEffect(() => {
    return installShortcuts({
      onView: (next) => {
        if (next === view) return;
        // Only offer layers the current mode surfaces.
        if (!visibleWorkspaceLayers(mode).some((l) => l.id === next)) return;
        window.history.pushState(null, "", getWorkspaceHref(next));
        setView(next);
        rememberView(next);
      },
      onNewSession: () => {
        window.dispatchEvent(new CustomEvent("sheytan:new-session"));
      },
      onFocusComposer: () => {
        const composer = document.querySelector<HTMLTextAreaElement>(
          ".composer-shell textarea, .composer textarea",
        );
        composer?.focus();
      },
      onToggleHelp: () => setHelpOpen((open) => !open),
    });
    // view/mode are read through refs via closures — re-register when they
    // change so the guard sees fresh values.
  }, [view, mode]);

  const statusLabel =
    connection === "connected"
      ? "Connected"
      : connection === "connecting"
        ? "Connecting"
        : connection === "error"
          ? "Connection error"
          : "Offline";

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark">S</div>

          <div className="brand-copy">
            <strong>SHEYTAN</strong>
            <span>Local Agent</span>
          </div>
        </div>

        <div className="topbar-status" title={statusLabel}>
          <span
            className={`status-dot ${
              connection === "connected" ? "ready" : ""
            }`}
          />
        </div>

        <div className="topbar-meta">
          <span>{appVersion ?? "v1.2.2"}</span>
        </div>
      </header>

      <div className="app-body">
        <aside className="sidebar">
          {/* v1.1.9: mode-aware navigation — Chat surfaces Workspace,
              Research, and Settings; Agent adds Coding Lab. One label per
              item, no repeated headings, no per-item descriptions. */}
          <nav className="app-navigation m-stagger" aria-label="Workspace">
            {visibleLayers.map((layer, index) => (
              <button
                type="button"
                key={layer.id}
                className={`app-navigation-item m-press ${
                  effectiveView === layer.id ? "active" : ""
                }`}
                style={{ "--stagger-index": index } as CSSProperties}
                onClick={() => changeView(layer.id)}
                aria-pressed={effectiveView === layer.id}
              >
                <span className="app-navigation-icon">{layer.icon}</span>

                <span className="app-navigation-copy">
                  <strong>{layer.label}</strong>
                </span>
              </button>
            ))}
          </nav>

          {effectiveView === "agent" ? (
            <Suspense fallback={<SidebarLayerLoading />}>
              <AgentSidebar />
            </Suspense>
          ) : null}

          <div className="sidebar-footer">
            <span>SHEYTAN Local-Agent</span>
          </div>
        </aside>

        <main className="workspace" key={effectiveView}>
          <section className="workspace-header view-transition-header">
            <div>
              <span className="eyebrow">{activeLayer.eyebrow}</span>

              {effectiveView === "agent" ? (
                <Suspense fallback={<h1>{activeLayer.title}</h1>}>
                  <AgentHeader />
                </Suspense>
              ) : (
                <h1>{activeLayer.title}</h1>
              )}
            </div>
          </section>

          <div className="view-transition">
            {/* v1.2.2: every lazy workspace panel sits inside its OWN error
                boundary — one panel's render failure (e.g. a malformed
                backend payload) shows that panel's recovery card instead of
                blanking the entire application. resetKey clears a stale
                failure whenever the user switches views. */}
            {effectiveView === "agent" ? (
              <PanelErrorBoundary label="Agent" resetKey="agent">
                <Suspense fallback={<PanelLoading label="Agent" />}>
                  <AgentBody />
                </Suspense>
              </PanelErrorBoundary>
            ) : effectiveView === "workspace" ? (
              <PanelErrorBoundary label="Workspace" resetKey="workspace">
                <Suspense fallback={<PanelLoading label="Workspace" />}>
                  <WorkspacePanel />
                </Suspense>
              </PanelErrorBoundary>
            ) : effectiveView === "lab" ? (
              <PanelErrorBoundary label="Coding Lab" resetKey="lab">
                <Suspense fallback={<PanelLoading label="Coding Lab" />}>
                  <LabPanel />
                </Suspense>
              </PanelErrorBoundary>
            ) : effectiveView === "research" ? (
              <PanelErrorBoundary label="Research" resetKey="research">
                <Suspense fallback={<PanelLoading label="Research" />}>
                  <ResearchPanel />
                </Suspense>
              </PanelErrorBoundary>
            ) : effectiveView === "system" ? (
              <PanelErrorBoundary label="System Centre" resetKey="system">
                <Suspense fallback={<PanelLoading label="System Centre" />}>
                  <SystemPanel />
                </Suspense>
              </PanelErrorBoundary>
            ) : (
              <PanelErrorBoundary label="Settings" resetKey="settings">
                <Suspense fallback={<PanelLoading label="Settings" />}>
                  <SettingsPanel />
                </Suspense>
              </PanelErrorBoundary>
            )}
          </div>
        </main>
      </div>

      {helpOpen ? <ShortcutHelpOverlay onClose={() => setHelpOpen(false)} /> : null}
    </div>
  );
}

export default App;
