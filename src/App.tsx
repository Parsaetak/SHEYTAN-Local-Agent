import { Suspense, lazy, useEffect, useState } from "react";
import type { CSSProperties } from "react";

import {
  getWorkspaceHref,
  getWorkspaceLayer,
  parseWorkspaceHash,
  type WorkspaceView,
  WORKSPACE_LAYERS,
} from "./workspace";
import { useRuntimeStore } from "./store";

const AgentBody = lazy(() => import("./AgentBody"));
const AgentHeader = lazy(() => import("./AgentHeader"));
const AgentSidebar = lazy(() => import("./AgentSidebar"));
const LabPanel = lazy(() => import("./LabPanel"));
const ResearchPanel = lazy(() => import("./ResearchPanel"));
const SettingsPanel = lazy(() => import("./SettingsPanel"));

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

  const activeLayer = getWorkspaceLayer(view);

  // v1.1.6 §13: a consistent SHEYTAN identity on every workspace layer
  // (window title / taskbar). "SHEYTAN", "SHEYTAN — Settings", … — never
  // generic shell or localhost strings.
  useEffect(() => {
    document.title =
      view === "agent" ? "SHEYTAN" : `SHEYTAN — ${activeLayer.label}`;
  }, [view, activeLayer.label]);

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

        <div className="topbar-status">
          <span
            className={`status-dot ${
              connection === "connected" ? "ready" : ""
            }`}
          />

          <span>{statusLabel}</span>
        </div>

        <div className="topbar-meta">
          <span>{appVersion ?? "v1.1.8"}</span>
        </div>
      </header>

      <div className="app-body">
        <aside className="sidebar">
          {/* v1.1.8: navigation de-chromed — one label per item, no
              repeated headings, no per-item descriptions. */}
          <nav className="app-navigation m-stagger" aria-label="Workspace">
            {WORKSPACE_LAYERS.map((layer, index) => (
              <button
                type="button"
                key={layer.id}
                className={`app-navigation-item m-press ${
                  view === layer.id ? "active" : ""
                }`}
                style={{ "--stagger-index": index } as CSSProperties}
                onClick={() => changeView(layer.id)}
                aria-pressed={view === layer.id}
              >
                <span className="app-navigation-icon">{layer.icon}</span>

                <span className="app-navigation-copy">
                  <strong>{layer.label}</strong>
                </span>
              </button>
            ))}
          </nav>

          {view === "agent" ? (
            <Suspense fallback={<SidebarLayerLoading />}>
              <AgentSidebar />
            </Suspense>
          ) : (
            <div className="sidebar-layer-info">
              <span className="eyebrow">LAYER</span>
              <strong>{activeLayer.title}</strong>
              <span>
                Only the active workspace loads its domain UI and data.
              </span>
            </div>
          )}

          <div className="sidebar-footer">
            <span>SHEYTAN™ Local-Agent</span>
            <span>Native runtime · offline</span>
          </div>
        </aside>

        <main className="workspace" key={view}>
          <section className="workspace-header view-transition-header">
            <div>
              <span className="eyebrow">{activeLayer.eyebrow}</span>

              {view === "agent" ? (
                <Suspense fallback={<h1>{activeLayer.title}</h1>}>
                  <AgentHeader />
                </Suspense>
              ) : (
                <h1>{activeLayer.title}</h1>
              )}
            </div>
          </section>

          <div className="view-transition">
            {view === "agent" ? (
              <Suspense fallback={<PanelLoading label="Agent" />}>
                <AgentBody />
              </Suspense>
            ) : view === "lab" ? (
              <Suspense fallback={<PanelLoading label="Coding Lab" />}>
                <LabPanel />
              </Suspense>
            ) : view === "research" ? (
              <Suspense fallback={<PanelLoading label="Research" />}>
                <ResearchPanel />
              </Suspense>
            ) : (
              <Suspense fallback={<PanelLoading label="Settings" />}>
                <SettingsPanel />
              </Suspense>
            )}
          </div>
        </main>
      </div>
    </div>
  );
}

export default App;
