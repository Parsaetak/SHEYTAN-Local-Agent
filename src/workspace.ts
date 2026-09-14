export type WorkspaceView = "agent" | "lab" | "research" | "settings";

export type WorkspaceMode = "chat" | "agent";

export type WorkspaceLayer = {
  id: WorkspaceView;
  label: string;
  eyebrow: string;
  title: string;
  description: string;
  icon: string;
  // v1.1.9: which top-level modes surface this layer in the navigation.
  // Coding Lab is Agent machinery — Chat stays a calm conversation
  // surface and does not offer it. Everything else is available in both
  // modes; the underlying runtime is shared either way.
  modes: readonly WorkspaceMode[];
};

const AGENT_LAYER: WorkspaceLayer = {
  id: "agent",
  label: "Workspace",
  eyebrow: "WORKSPACE",
  title: "SHEYTAN",
  description: "Interactive local intelligence",
  icon: "◈",
  modes: ["chat", "agent"],
};

export const WORKSPACE_LAYERS: readonly WorkspaceLayer[] = [
  AGENT_LAYER,
  {
    id: "lab",
    label: "Coding Lab",
    eyebrow: "CODING LAB",
    title: "Autonomous engineering",
    description: "Execute, verify, and repair",
    icon: "◆",
    modes: ["agent"],
  },
  {
    id: "research",
    label: "Research",
    eyebrow: "RESEARCH",
    title: "External intelligence",
    description: "External evidence and sources",
    icon: "⌕",
    modes: ["chat", "agent"],
  },
  {
    id: "settings",
    label: "Settings",
    eyebrow: "SETTINGS",
    title: "Settings",
    description: "Models, engine, agent, and integrations",
    icon: "⚙",
    modes: ["chat", "agent"],
  },
] as const;

// v1.1.9: the navigation is mode-aware — Chat hides Agent machinery
// (Coding Lab) without touching the layers themselves.
export function visibleWorkspaceLayers(
  mode: WorkspaceMode,
): readonly WorkspaceLayer[] {
  return WORKSPACE_LAYERS.filter((layer) => layer.modes.includes(mode));
}

export function isWorkspaceView(value: string): value is WorkspaceView {
  return WORKSPACE_LAYERS.some((layer) => layer.id === value);
}

export function getWorkspaceLayer(view: WorkspaceView): WorkspaceLayer {
  return WORKSPACE_LAYERS.find((layer) => layer.id === view) ?? AGENT_LAYER;
}

export function parseWorkspaceHash(): WorkspaceView {
  if (typeof window === "undefined") {
    return "agent";
  }

  const hash = window.location.hash.replace(/^#/, "").trim().toLowerCase();

  return isWorkspaceView(hash) ? hash : "agent";
}

export function workspaceHash(view: WorkspaceView): string {
  return view === "agent" ? "" : `#${view}`;
}

export function getWorkspaceHref(view: WorkspaceView): string {
  const hash = workspaceHash(view);

  return `${window.location.pathname}${window.location.search}${hash}`;
}
