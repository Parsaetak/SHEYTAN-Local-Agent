import { Suspense, lazy, useEffect, useState } from "react";
import type { CSSProperties } from "react";

import {
  getWorkspaceHref,
  getWorkspaceLayer,
  parseWorkspaceHash,
  visibleWorkspaceLayers,
  type WorkspaceView,
} from "./workspace";
import { PanelErrorBoundary } from "./ErrorBoundary";
import { useRuntimeStore } from "./store";

const AgentBody = lazy(() => import("./AgentBody"));
const AgentHeader = lazy(() => import("./AgentHeader"));
const AgentSidebar = lazy(() => import("./AgentSidebar"));
const LabPanel = lazy(() => import("./LabPanel"));
const ResearchPanel = lazy(() => import("./ResearchPanel"));
const SettingsPanel = lazy(() => import("./SettingsPanel"));
const SystemPanel = lazy(() => import("./SystemPanel"));

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

function App() {
  const appVersion = useRuntimeStore((state) => state.app?.appVersion ?? null);
  const connection = useRuntimeStore((state) => state.connection);
  // v1.1.9: the navigation is mode-aware — Chat hides Agent machinery
  // (Coding Lab) so the primary surface stays minimal.
  const mode = useRuntimeStore((state) => state.mode);

  const [view, setView] = useState<WorkspaceView>(() => parseWorkspaceHash());

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
    </div>
  );
}

export default App;
