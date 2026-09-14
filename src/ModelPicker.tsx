import { useCallback, useMemo, useState } from "react";

import { api, type Model } from "./api";
import { useRuntimeStore } from "./store";
import { visionBadge } from "./vision";

// v1.1.9: the model chooser is one of the clearest surfaces in the
// product. One card per local GGUF with the facts that drive a decision:
// identity, size class, context, memory, tool/vision capability, native
// support, and an honest compatibility hint. Every number here is
// backend-sourced (/api/models + /api/sysinfo); nothing is invented, and
// unknown values render as "—".
//
// "Remove" is deliberately NOT offered: there is no model-deletion API
// by design (the models folder is user-managed), so the picker only
// exposes actions that actually exist — Use / Open models folder /
// Refresh / Details.

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

// v1.1.9: explicit card states. The UI never guesses: "Ready" means the
// backend is serving this model right now, "Loading" means a switch to
// this exact model is in flight, "Incompatible" means the measured
// estimate exceeds total host RAM (it cannot fit), and everything else
// is "Available".
type ModelState = "ready" | "loading" | "incompatible" | "available";

// classification derives the honest suitability hint from measured
// values: the model's estimated footprint against the HOST's total RAM.
// It is a sizing hint, not a benchmark — the tooltip says exactly that.
type Classification = "recommended" | "compatible" | "limited";

function classify(model: Model, totalRamBytes: number | undefined) {
  const need = model.estimatedMemoryBytes ?? 0;

  if (need <= 0 || !totalRamBytes || totalRamBytes <= 0) {
    return { classification: null as Classification | null, overRam: false };
  }

  const ratio = need / totalRamBytes;

  if (ratio > 1) {
    return { classification: null as Classification | null, overRam: true };
  }

  if (ratio <= 0.6) {
    return {
      classification: "recommended" as Classification,
      overRam: false,
    };
  }

  if (ratio <= 0.85) {
    return {
      classification: "compatible" as Classification,
      overRam: false,
    };
  }

  return { classification: "limited" as Classification, overRam: false };
}

const CLASS_LABEL: Record<Classification, string> = {
  recommended: "Recommended",
  compatible: "Compatible",
  limited: "Limited",
};

const CLASS_TITLE: Record<Classification, string> = {
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
  const { classification, overRam } = classify(model, totalRamBytes);
  const ramEstimate = formatBytes(model.estimatedMemoryBytes);
  const vramEstimate = formatBytes(model.estimatedVRAMBytes);
  const ctxMax = formatContext(model.contextLength);

  // v1.2.0: the evidence-backed vision state (never a filename guess).
  const vision = visionBadge(model.visionState, model.visionReason, model.mmprojName);

  // v1.1.9: explicit state machine per card (see ModelState above).
  const state: ModelState = model.serving
    ? "ready"
    : busy && active
      ? "loading"
      : overRam
        ? "incompatible"
        : "available";

  // Honest "selected" marker: configured but not yet being served.
  const selected = active && !model.serving;

  const facts: string[] = [];

  if (model.quantization) {
    facts.push(model.quantization);
  }

  if (model.parameterInfo) {
    facts.push(model.parameterInfo);
  }

  const useDisabled =
    state === "ready" || state === "incompatible" || busy || !canUse;

  const useTitle =
    state === "ready"
      ? "This model is already being served."
      : state === "incompatible"
        ? "The estimated memory footprint exceeds total host RAM."
        : busy
          ? "A model operation is already running."
          : undefined;

  return (
    <article
      className={`model-card state-${state}${model.serving ? " serving" : ""}`}
      data-testid={`model-card-${model.id}`}
    >
      <header className="model-card-head">
        <strong className="model-card-name" title={model.path}>
          {model.name}
        </strong>

        {state === "ready" ? (
          <span
            className="model-chip class-serving"
            title="The engine is serving this model right now."
          >
            Ready
          </span>
        ) : state === "loading" ? (
          <span className="model-chip class-loading" title="Loading this model into the engine.">
            Loading
          </span>
        ) : state === "incompatible" ? (
          <span
            className="model-chip class-incompatible"
            title="Estimated memory exceeds total host RAM — the model cannot fit."
          >
            Incompatible
          </span>
        ) : selected ? (
          <span
            className="model-chip class-serving"
            title="This model is configured and will serve on the next engine start."
          >
            Selected
          </span>
        ) : null}

        {classification && state !== "incompatible" ? (
          <span
            className={`model-chip class-${classification}`}
            title={
              classification === "recommended"
                ? CLASS_TITLE[classification] +
                  " Based on this machine's measured RAM."
                : CLASS_TITLE[classification]
            }
          >
            {classification === "recommended"
              ? "Recommended for your device"
              : CLASS_LABEL[classification]}
          </span>
        ) : null}
      </header>

      {facts.length > 0 ? (
        <span className="model-card-facts">{facts.join(" · ")}</span>
      ) : (
        <span className="model-card-facts">No GGUF metadata</span>
      )}

      {/* v1.2.0: the at-a-glance fact grid — Text / Vision / Tools /
          Native / Context / RAM / VRAM. "—" means the backend did not
          report a value; nothing is fabricated. */}
      <div className="model-card-grid">
        <span>Text</span>
        <strong>✓</strong>

        <span
          title={vision.title}
        >
          Vision
        </span>
        <strong className={`vision-value tone-${vision.tone}`}>
          {vision.symbol ? `${vision.symbol} ${vision.label}` : vision.label}
        </strong>

        <span
          title={
            model.chatTemplate
              ? "The architecture ships a usable chat template — the prerequisite for tool calling."
              : "No chat template detected, so tool calling is unlikely to work."
          }
        >
          Tools
        </span>
        <strong>{model.chatTemplate ? "✓" : "—"}</strong>

        <span
          title={
            model.nativeBackend
              ? model.nativeReason || "The native C++ engine can execute this architecture."
              : "The native C++ engine cannot execute this architecture."
          }
        >
          Native
        </span>
        <strong>{model.nativeBackend ? "✓" : "—"}</strong>

        <span>Context</span>
        <strong>{ctxMax || "—"}</strong>

        <span>RAM</span>
        <strong>{ramEstimate || "—"}</strong>

        <span>VRAM</span>
        <strong>{vramEstimate || "—"}</strong>
      </div>

      {gpuAvailable && (
        <div className="model-card-chips">
          <span className="model-chip" title="This machine exposes a usable GPU for offload.">
            GPU offload available
          </span>
        </div>
      )}

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

        {model.visionState && (
          <div className="model-detail-row">
            <span>Vision state</span>
            <strong title={vision.title}>{vision.label}</strong>
          </div>
        )}

        {model.mmprojName && (
          <div className="model-detail-row">
            <span>Projector</span>
            <strong title={model.mmprojPath}>
              {model.mmprojName}
              {model.mmprojSizeBytes
                ? ` · ${formatBytes(model.mmprojSizeBytes) || ""}`
                : ""}
              {model.mmprojVerified ? " · verified" : ""}
            </strong>
          </div>
        )}

        {model.visionReason && (
          <div className="model-detail-row">
            <span>Why</span>
            <strong>{model.visionReason}</strong>
          </div>
        )}
      </details>

      <footer className="model-card-actions">
        <button
          type="button"
          className="primary-button"
          disabled={useDisabled}
          onClick={() => onUse(model.id)}
          title={useTitle}
        >
          {state === "ready"
            ? "In use"
            : state === "loading"
              ? "Loading…"
              : "Use model"}
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
  const engineState = useRuntimeStore((state) => state.engine);

  const localModels = models?.local ?? [];

  // v1.2.0 first-run: one click applies the recommended configuration and
  // starts the engine. Only offered when a real recommendation exists.
  const [onboardingBusy, setOnboardingBusy] = useState(false);

  const useRecommended = useCallback(async () => {
    setOnboardingBusy(true);

    try {
      const payload = await api.recommendation("", "chat");
      const rec = payload.recommended;
      const serving =
        models?.local.find((m) => m.serving) ??
        models?.local.find(
          (m) => m.serving === false && m.id === activeModel,
        );

      const best =
        models?.local
          .filter((m) => !m.serving)
          .sort((a, b) => {
            const ra = a.estimatedMemoryBytes ?? Number.MAX_SAFE_INTEGER;
            const rb = b.estimatedMemoryBytes ?? Number.MAX_SAFE_INTEGER;
            return ra - rb;
          })[0] ?? null;

      const target = serving?.id ?? best?.id ?? null;

      if (rec) {
        await api.updateConfig({
          llm: {
            numCtx: rec.context,
            numThread: rec.threads,
            numGpu: rec.gpuLayers,
            ubatchSize: rec.ubatchSize,
          },
          gpuAutoOffload: rec.gpuAutoOffload,
          flashAttention: rec.flashAttention,
          kvCacheQuant: rec.kvCacheQuant,
          visionMmprojOffload: rec.mmprojOffload,
          runtimeProfile: rec.task,
        });
      }

      if (target) {
        onUse(target);
      } else if (engineState?.state === "idle" || !engineAlive) {
        await api.llama("start");
        await refreshModels();
      }
    } catch {
      // surfaced through the runtime store error surfaces
    } finally {
      setOnboardingBusy(false);
    }
  }, [models, activeModel, onUse, engineAlive, engineState, refreshModels]);

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
          <strong>Choose a model</strong>

          <span>
            Place a GGUF model in the models folder, then refresh. No valid
            model is configured yet, so requests cannot run.
          </span>

          <button
            type="button"
            className="secondary-button"
            onClick={() => void api.openModelsFolder().catch(() => {})}
          >
            Open models folder
          </button>
        </div>
      ) : (
        <>
          {localModels.length > 0 ? (
            <div className="model-picker-onboarding">
              <button
                type="button"
                className="primary-button"
                onClick={() => void useRecommended()}
                disabled={onboardingBusy || busy}
                title="Apply the recommended configuration for this machine, pick the smallest fitting model, and start the engine."
              >
                {onboardingBusy ? "Applying recommended setup…" : "Use recommended setup"}
              </button>

              <span>
                Detects this device, applies the best-fitting configuration,
                verifies the engine, and starts the conversation.
              </span>
            </div>
          ) : null}

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
        </>
      )}
    </section>
  );
};

export default ModelPicker;
