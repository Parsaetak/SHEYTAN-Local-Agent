import { useMemo } from "react";

import { api, type Model } from "./api";
import { useRuntimeStore } from "./store";

// v1.1.8: the redesigned model-selection panel. One card per local GGUF
// with only the facts that drive a decision — identity, size class,
// context, memory estimate, and measured capabilities. Every number here
// is backend-sourced (/api/models + /api/sysinfo); nothing is invented.
//
// "Remove" is deliberately NOT offered: there is no model-deletion API
// by design (the models folder is user-managed), so the picker only
// exposes actions that actually exist — Use / Open folder / Refresh /
// Details.

function formatBytes(bytes: number | undefined): string {
  if (!bytes || bytes <= 0) {
    return "";
  }

  if (bytes >= 1024 ** 3) {
    return `~${(bytes / 1024 ** 3).toFixed(1)} GB`;
  }

  return `~${Math.round(bytes / 1024 ** 2)} MB`;
}

function formatContext(tokens: number | undefined): string {
  if (!tokens || tokens <= 0) {
    return "";
  }

  if (tokens >= 1024) {
    const k = tokens / 1024;
    return `${k % 1 === 0 ? k : k.toFixed(1)}K`;
  }

  return String(tokens);
}

// classification derives the honest suitability chip from measured
// values: the model's estimated footprint against the HOST's total RAM.
// It is a sizing hint, not a benchmark — the tooltip says exactly that.
type Classification = "serving" | "recommended" | "compatible" | "limited";

function classify(
  model: Model,
  totalRamBytes: number | undefined,
): Classification | null {
  if (model.serving) {
    return "serving";
  }

  const need = model.estimatedMemoryBytes ?? 0;

  if (need <= 0 || !totalRamBytes || totalRamBytes <= 0) {
    return null;
  }

  const ratio = need / totalRamBytes;

  if (ratio <= 0.6) {
    return "recommended";
  }

  if (ratio <= 0.85) {
    return "compatible";
  }

  return "limited";
}

const CLASS_LABEL: Record<Classification, string> = {
  serving: "In use",
  recommended: "Recommended",
  compatible: "Compatible",
  limited: "Limited",
};

const CLASS_TITLE: Record<Classification, string> = {
  serving: "The active backend is serving this model right now.",
  recommended: "Estimated footprint fits comfortably in host RAM.",
  compatible: "Estimated footprint fits host RAM with little headroom.",
  limited: "Estimated footprint exceeds comfortable host RAM — expect slowdowns.",
};

function ModelCard({
  model,
  active,
  busy,
  totalRamBytes,
  gpuAvailable,
  canUse,
  onUse,
}: {
  model: Model;
  /** The configured "next" model (config → engine → session). */
  active: boolean;
  busy: boolean;
  totalRamBytes: number | undefined;
  gpuAvailable: boolean;
  canUse: boolean;
  onUse: (id: string) => void;
}) {
  const classification = classify(model, totalRamBytes);
  const ramEstimate = formatBytes(model.estimatedMemoryBytes);
  const vramEstimate = formatBytes(model.estimatedVRAMBytes);
  const ctxMax = formatContext(model.contextLength);

  // Honest "selected" marker: configured but not yet being served.
  const selected = active && !model.serving;

  const facts: string[] = [];

  if (model.quantization) {
    facts.push(model.quantization);
  }

  if (model.parameterInfo) {
    facts.push(model.parameterInfo);
  }

  if (ctxMax) {
    facts.push(`Context ${ctxMax}`);
  }

  if (ramEstimate) {
    facts.push(`RAM ${ramEstimate}`);
  }

  return (
    <article
      className={`model-card${model.serving ? " serving" : ""}`}
      data-testid={`model-card-${model.id}`}
    >
      <header className="model-card-head">
        <strong className="model-card-name" title={model.path}>
          {model.name}
        </strong>

        {classification ? (
          <span
            className={`model-chip class-${classification}`}
            title={CLASS_TITLE[classification]}
          >
            {CLASS_LABEL[classification]}
          </span>
        ) : null}

        {selected ? (
          <span
            className="model-chip class-serving"
            title="This model is configured and will serve on the next engine start."
          >
            Selected
          </span>
        ) : null}
      </header>

      {facts.length > 0 ? (
        <span className="model-card-facts">{facts.join(" · ")}</span>
      ) : (
        <span className="model-card-facts">No GGUF metadata</span>
      )}

      <div className="model-card-chips">
        {gpuAvailable && (
          <span className="model-chip" title="This machine exposes a usable GPU for offload.">
            GPU offload available
          </span>
        )}

        {model.multimodal && (
          <span className="model-chip" title="A vision projector (mmproj) is paired with this model.">
            Vision
          </span>
        )}

        {model.chatTemplate && (
          <span className="model-chip" title="The architecture ships a usable chat template — the prerequisite for tool calling.">
            Chat template
          </span>
        )}

        {model.nativeBackend && (
          <span
            className="model-chip"
            title={model.nativeReason || "The native C++ engine can execute this architecture."}
          >
            Native: Supported
          </span>
        )}
      </div>

      <details className="model-card-details">
        <summary>Details</summary>

        <div className="model-detail-row">
          <span>File</span>
          <strong title={model.path}>{model.id}</strong>
        </div>

        {model.architecture && (
          <div className="model-detail-row">
            <span>Architecture</span>
            <strong>{model.architecture}</strong>
          </div>
        )}

        {model.sizeBytes ? (
          <div className="model-detail-row">
            <span>File size</span>
            <strong>{formatBytes(model.sizeBytes) || `${model.sizeBytes} B`}</strong>
          </div>
        ) : null}

        {model.recommendedContext ? (
          <div className="model-detail-row">
            <span>Recommended context</span>
            <strong>{model.recommendedContext.toLocaleString()} tok</strong>
          </div>
        ) : null}

        {vramEstimate && (
          <div className="model-detail-row">
            <span>VRAM estimate</span>
            <strong>{vramEstimate}</strong>
          </div>
        )}

        {model.nativeReason && (
          <div className="model-detail-row">
            <span>Native support</span>
            <strong>{model.nativeReason}</strong>
          </div>
        )}
      </details>

      <footer className="model-card-actions">
        <button
          type="button"
          className="primary-button"
          disabled={model.serving || busy || !canUse}
          onClick={() => onUse(model.id)}
          title={
            model.serving
              ? "This model is already being served."
              : busy
                ? "A model operation is already running."
                : undefined
          }
        >
          {model.serving ? "In use" : busy ? "Working…" : "Use model"}
        </button>
      </footer>
    </article>
  );
}

const ModelPicker = function ModelPicker({
  activeModel,
  busy,
  onUse,
  onClose,
}: {
  /** Model id the runtime would serve next (config → engine → session). */
  activeModel: string | null;
  /** True while a model switch is in flight. */
  busy: boolean;
  /** Apply a model (owned by the caller — one implementation, shared). */
  onUse: (id: string) => void;
  /** When provided, the panel shows a close affordance. */
  onClose?: () => void;
}) {
  const models = useRuntimeStore((state) => state.models);
  const sysinfo = useRuntimeStore((state) => state.sysinfo);
  const refreshModels = useRuntimeStore((state) => state.refreshModels);
  const engineAlive = useRuntimeStore((state) => state.engine?.state);

  const localModels = models?.local ?? [];

  const sorted = useMemo(() => {
    // Serving model first, then alphabetical — a stable, predictable list.
    return [...localModels].sort((a, b) => {
      if (a.serving !== b.serving) {
        return a.serving ? -1 : 1;
      }

      return a.name.localeCompare(b.name);
    });
  }, [localModels]);

  const totalRamBytes = sysinfo?.ram?.totalBytes;
  const gpuAvailable = sysinfo?.recommended?.canRunGPU === true;

  return (
    <section className="model-picker" aria-label="Model selection">
      <header className="model-picker-head">
        <div>
          <span className="eyebrow">MODELS</span>
          <strong>
            {localModels.length > 0
              ? `${localModels.length} local model${localModels.length === 1 ? "" : "s"}`
              : "No local models"}
          </strong>
        </div>

        <div className="model-picker-tools">
          <button
            type="button"
            className="text-button"
            onClick={() => void refreshModels()}
            disabled={busy}
          >
            Refresh
          </button>

          <button
            type="button"
            className="text-button"
            onClick={() => void api.openModelsFolder().catch(() => {})}
          >
            Open models folder
          </button>

          {onClose ? (
            <button type="button" className="text-button" onClick={onClose}>
              Close
            </button>
          ) : null}
        </div>
      </header>

      {sorted.length === 0 ? (
        <div className="model-picker-empty">
          <strong>No GGUF models found</strong>

          <span>
            Place model files in the models folder, then refresh. The picker
            reads each file&apos;s header to show architecture, context, and
            memory facts.
          </span>
        </div>
      ) : (
        <div className="model-picker-grid">
          {sorted.map((model) => (
            <ModelCard
              key={model.id}
              model={model}
              active={model.serving || model.id === activeModel}
              busy={busy}
              totalRamBytes={totalRamBytes}
              gpuAvailable={gpuAvailable}
              canUse={engineAlive !== "downloading"}
              onUse={onUse}
            />
          ))}
        </div>
      )}
    </section>
  );
};

export default ModelPicker;
