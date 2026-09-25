import { memo, useCallback, useEffect, useMemo, useState } from "react";

import { api, type Model } from "./api";
import {
  SELECTION_PHASE_LABEL,
  classifyFit,
  recommendedOnThisMachine,
  selectionBusy,
  type Fit,
  type ModelRecommendationEvidence,
  type SelectionPhase,
} from "./model-selection";
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
// v1.5.0: classification is now TWO separate honest signals —
//
//   fit       — a MEASURED sizing hint: the backend's estimated
//               footprint against the host's measured total RAM. It
//               answers "can this machine hold it?", nothing more.
//   evidence  — the recommendation engine's verdict over measured
//               hardware + GGUF facts + verified engine capabilities.
//               "Recommended for this machine" is rendered ONLY when
//               this evidence exists and classifies the model as safe.
//
// No label is ever derived from the filename, the file size alone, or a
// client-side heuristic dressed up as a recommendation.


const FIT_LABEL: Record<Exclude<Fit, null>, string> = {
  fits: "Fits host RAM",
  tight: "Tight on host RAM",
  over: "Exceeds host RAM",
};

const FIT_TITLE: Record<Exclude<Fit, null>, string> = {
  fits: "The backend's estimated footprint fits the host's measured RAM — a sizing hint, not a benchmark.",
  tight: "The estimated footprint leaves little measured RAM headroom — expect pressure.",
  over: "The estimated memory footprint exceeds the host's measured total RAM — the model cannot fit.",
};



function ModelCard({
  model,
  active,
  previous,
  busy,
  selectionBusy,
  totalRamBytes,
  gpuAvailable,
  evidence,
  canUse,
  onUse,
}: {
  model: Model;
  /** The configured "next" model (config → engine → session). */
  active: boolean;
  /** v1.5.0: the last-but-one selection (the "previous" marker). */
  previous: boolean;
  busy: boolean;
  /** v1.5.0: a backend selection flow is in flight (cards lock). */
  selectionBusy: boolean;
  totalRamBytes: number | undefined;
  gpuAvailable: boolean;
  /** v1.5.0: the recommendation engine's measured evidence (or null). */
  evidence: ModelRecommendationEvidence | null;
  canUse: boolean;
  onUse: (id: string) => void;
}) {
  const fit = classifyFit(model.estimatedMemoryBytes, totalRamBytes);
  const overRam = fit === "over";
  const ramEstimate = formatBytes(model.estimatedMemoryBytes);
  const vramEstimate = formatBytes(model.estimatedVRAMBytes);
  const ctxMax = formatContext(model.contextLength);

  // v1.2.0: the evidence-backed vision state (never a filename guess).
  const vision = visionBadge(model.visionState, model.visionReason, model.mmprojName);

  // v1.1.9: explicit state machine per card (see ModelState above).
  // v1.5.0: a live backend selection targeting THIS model also shows
  // the loading state (the phase is backend-authoritative).
  const state: ModelState = model.serving
    ? "ready"
    : (busy || selectionBusy) && active
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
    state === "ready" ||
    state === "incompatible" ||
    busy ||
    selectionBusy ||
    !canUse;

  const useTitle =
    state === "ready"
      ? "This model is already being served."
      : state === "incompatible"
        ? "The estimated memory footprint exceeds total host RAM."
        : selectionBusy
          ? "A model selection is being applied."
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

        {previous && !model.serving && !selected ? (
          <span
            className="model-chip class-previous"
            title="The previously selected model — what this machine served before the current selection."
          >
            Previous
          </span>
        ) : null}

        {/* v1.5.0: "Recommended for this machine" ONLY from the
            recommendation engine's measured evidence — never from the
            filename, the size, or a client-side heuristic. */}
        {recommendedOnThisMachine(evidence) && state !== "incompatible" ? (
          <span
            className="model-chip class-recommended"
            title={
              (evidence?.reasons ?? []).join("\n") ||
              "The recommendation engine classified this model as a safe fit for this machine's measured hardware."
            }
          >
            Recommended for this machine
          </span>
        ) : null}

        {fit && fit !== "over" && state !== "incompatible" ? (
          <span
            className={`model-chip class-fit-${fit}`}
            title={FIT_TITLE[fit]}
          >
            {FIT_LABEL[fit]}
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
  const modelsLoading = useRuntimeStore((state) => state.modelsLoading);
  const sysinfo = useRuntimeStore((state) => state.sysinfo);
  const refreshModels = useRuntimeStore((state) => state.refreshModels);
  const engineAlive = useRuntimeStore((state) => state.engine?.state);
  const engineState = useRuntimeStore((state) => state.engine);

  const localModels = models?.local ?? [];

  // v1.5.0: the per-model recommendation evidence — fetched ONCE per
  // picker open (one hardware probe shared across models on the
  // backend). This is the ONLY source of the "Recommended for this
  // machine" label; when the fetch fails the chips simply do not
  // render (never a client-side fallback heuristic).
  const [evidence, setEvidence] = useState<
    Record<string, ModelRecommendationEvidence> | null
  >(null);

  useEffect(() => {
    if (localModels.length === 0) {
      setEvidence(null);
      return;
    }

    let cancelled = false;
    const controller = new AbortController();

    void api
      .modelsRecommendations(controller.signal)
      .then((payload) => {
        if (!cancelled) {
          setEvidence(payload.recommendations);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setEvidence(null);
        }
      });

    return () => {
      cancelled = true;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [models?.local.map((m) => m.id).join("|")]);

  // v1.5.0: the backend-authoritative selection flow state. While a
  // selection is in flight the cards lock and the phase is shown as a
  // status line — the UI never invents a phase the backend did not
  // report.
  const selection = engineState?.selection ?? null;
  const selectionPhase = (selection?.phase ?? null) as SelectionPhase | null;
  const selectionInFlight = selectionBusy(selectionPhase);

  // v1.2.0 first-run: one click applies the recommended setup. v1.5.0:
  // the backend select flow owns the whole chain (analyze → configure
  // atomically → load → verify); the button just picks the target and
  // hands it to the SAME onUse path as a manual card click.
  const [onboardingBusy, setOnboardingBusy] = useState(false);

  const useRecommended = useCallback(async () => {
    setOnboardingBusy(true);

    try {
      const serving =
        models?.local.find((m) => m.serving) ??
        models?.local.find(
          (m) => m.serving === false && m.id === activeModel,
        );

      // v1.5.0: the recommendation EVIDENCE decides the target — the
      // first model the recommendation engine classified as a safe fit
      // for this machine; the smallest measured footprint is the
      // fallback when nothing is evidence-classified.
      const evidenceSafe =
        models?.local.find(
          (m) =>
            !m.serving && evidence?.[m.id]?.hardwareMeasured === true && evidence[m.id]?.class === "safe",
        ) ?? null;

      const best =
        evidenceSafe ??
        models?.local
          .filter((m) => !m.serving)
          .sort((a, b) => {
            const ra = a.estimatedMemoryBytes ?? Number.MAX_SAFE_INTEGER;
            const rb = b.estimatedMemoryBytes ?? Number.MAX_SAFE_INTEGER;
            return ra - rb;
          })[0] ??
        null;

      const target = serving?.id ?? best?.id ?? null;

      if (target) {
        onUse(target);
      }
    } catch {
      // surfaced through the runtime store error surfaces
    } finally {
      setOnboardingBusy(false);
    }
  }, [models, activeModel, evidence, onUse]);

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

      {sorted.length === 0 && modelsLoading ? (
        <div className="model-picker-skeleton" aria-busy="true" aria-live="polite">
          <strong>Loading models…</strong>
          <div className="model-card-skeleton m-skeleton" />
          <div className="model-card-skeleton m-skeleton" />
          <div className="model-card-skeleton m-skeleton" />
        </div>
      ) : sorted.length === 0 ? (
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
          {/* v1.5.0: the backend-authoritative selection status — the
              phases the engine actually reported (Select / Analyzing /
              Configuring / Loading / Calibrating / Ready / Failed),
              never a client-side guess. */}
          {selection ? (
            <div
              className={`model-picker-selection phase-${selection.phase}`}
              role="status"
              aria-live="polite"
              data-testid="selection-status"
            >
              <strong>
                {SELECTION_PHASE_LABEL[selection.phase] ?? selection.phase}
                {selection.model ? ` · ${selection.model}` : ""}
              </strong>

              {selection.phase === "failed" && selection.error ? (
                <span className="selection-error">{selection.error}</span>
              ) : null}

              {selection.phase !== "failed" && selection.calibrationNote ? (
                <span>{selection.calibrationNote}</span>
              ) : null}

              {selection.performanceMode === "manual" && selection.phase !== "failed" ? (
                <span className="selection-manual">
                  Manual mode — your explicit settings are preserved.
                </span>
              ) : null}
            </div>
          ) : null}

          {localModels.length > 0 ? (
            <div className="model-picker-onboarding">
              <button
                type="button"
                className="primary-button"
                onClick={() => void useRecommended()}
                disabled={onboardingBusy || busy || selectionInFlight}
                title="Pick the evidence-recommended model for this machine; SHEYTAN analyzes, configures and verifies it automatically."
              >
                {onboardingBusy ? "Applying recommended setup…" : "Use recommended setup"}
              </button>

              <span>
                Analyzes this machine and the selected model, applies the
                automatic configuration, verifies the engine, and starts the
                conversation.
              </span>
            </div>
          ) : null}

          <div className="model-picker-grid">
            {sorted.map((model) => (
              <ModelCard
                key={model.id}
                model={model}
                active={model.serving || model.id === activeModel}
                previous={model.previous === true}
                busy={busy}
                selectionBusy={selectionInFlight}
                totalRamBytes={totalRamBytes}
                gpuAvailable={gpuAvailable}
                evidence={evidence?.[model.id] ?? null}
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

export default memo(ModelPicker);
