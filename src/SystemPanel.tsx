import { useCallback, useEffect, useState } from "react";

import {
  api,
  type EnvironmentPayload,
  type HealthPayload,
} from "./api";

// v1.2.0 — the Environment Centre (System Centre): one honest view over
// device, runtime, verified health and the recommendation engine. Every
// value comes from /api/environment + /api/health (existing telemetry);
// unknown values render as "—".

function fmtBytes(bytes: number | undefined): string {
  if (!bytes || bytes <= 0) return "—";
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  return `${Math.round(bytes / 1024 ** 2)} MB`;
}

const HEALTH_TONE: Record<string, string> = {
  ok: "good",
  warn: "warn",
  fail: "bad",
  unknown: "neutral",
  na: "neutral",
};

const HEALTH_SYMBOL: Record<string, string> = {
  ok: "✓",
  warn: "!",
  fail: "✕",
  unknown: "?",
  na: "–",
};

function DeviceCard({ env }: { env: EnvironmentPayload | null }) {
  if (!env) {
    return (
      <div className="settings-card">
        <span className="eyebrow">DEVICE</span>
        <p className="env-loading">Measuring this machine…</p>
      </div>
    );
  }

  const { device } = env;

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">DEVICE</span>
          <strong>{device.identity.shortName}</strong>
        </div>
        <span className="settings-chip chip-neutral">
          {device.os || "—"} · {device.arch || "—"}
        </span>
      </div>

      <div className="env-grid">
        <div className="session-detail">
          <span>CPU</span>
          <strong title={device.cpu.name}>
            {device.cpu.name || "—"}
            {device.cpu.physicalCores
              ? ` · ${device.cpu.physicalCores}C/${device.cpu.logicalCores ?? "?"}T`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>RAM</span>
          <strong>
            {fmtBytes(device.ram.totalBytes)}
            {device.ram.availableBytes
              ? ` · ${fmtBytes(device.ram.availableBytes)} available`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>GPU</span>
          <strong>
            {device.gpus.length > 0
              ? device.gpus
                  .map((g) => `${g.name || g.vendor || "GPU"}${g.vramBytes ? ` (${fmtBytes(g.vramBytes)})` : ""}`)
                  .join(", ")
              : "— none detected"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Storage</span>
          <strong>
            {device.storage.freeBytes
              ? `${fmtBytes(device.storage.freeBytes)} free of ${fmtBytes(device.storage.totalBytes)}`
              : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Engine backend</span>
          <strong>
            {device.backend.engineBinary
              ? `provisioned${device.backend.vulkan ? " · Vulkan" : ""}`
              : "not yet provisioned (auto-download on first run)"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Identity</span>
          <strong title={device.identity.appUserModelId}>
            {device.identity.product} · {device.identity.appUserModelId || device.identity.platform}
          </strong>
        </div>
      </div>
    </div>
  );
}

function RuntimeCard({ env }: { env: EnvironmentPayload | null }) {
  if (!env) return null;

  const { runtime } = env;

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">RUNTIME</span>
          <strong>{runtime.backend || "engine"}</strong>
        </div>
        <span className={`settings-chip chip-${runtime.verified ? "good" : "warn"}`}>
          {runtime.verified ? "✓ verified" : "not verified"}
        </span>
      </div>

      <div className="env-grid">
        <div className="session-detail">
          <span>Engine</span>
          <strong>
            {runtime.engineState}
            {runtime.enginePhase && runtime.enginePhase !== "ready"
              ? ` · ${runtime.enginePhase}`
              : ""}
          </strong>
        </div>

        <div className="session-detail">
          <span>Model</span>
          <strong>{runtime.model || "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Context</span>
          <strong>{runtime.context ? `${runtime.context.toLocaleString()} tok` : "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Vision</span>
          <strong
            title={runtime.vision.reason}
            className={`vision-value tone-${
              runtime.vision.state === "ready"
                ? "good"
                : runtime.vision.state === "failed"
                  ? "bad"
                  : "warn"
            }`}
          >
            {runtime.vision.state
              ? `${runtime.vision.active ? "✓ " : ""}${runtime.vision.state}`
              : "—"}
          </strong>
        </div>

        <div className="session-detail">
          <span>Engine tag</span>
          <strong>{runtime.engineTag || "—"}</strong>
        </div>

        <div className="session-detail">
          <span>Profile</span>
          <strong>{env.recommendation.task || "chat"}</strong>
        </div>
      </div>
    </div>
  );
}

function RecommendationCard({ env }: { env: EnvironmentPayload | null }) {
  const [applying, setApplying] = useState(false);
  const [appliedAt, setAppliedAt] = useState<string | null>(null);

  if (!env) return null;

  const rec = env.recommendation;

  const apply = useCallback(async () => {
    setApplying(true);

    try {
      const payload = await api.recommendation("", rec.task);
      const r = payload.recommended;
      if (!r) return;

      await api.updateConfig({
        llm: {
          numCtx: r.context,
          numThread: r.threads,
          numGpu: r.gpuLayers,
          ubatchSize: r.ubatchSize,
        },
        gpuAutoOffload: r.gpuAutoOffload,
        flashAttention: r.flashAttention,
        kvCacheQuant: r.kvCacheQuant,
        visionMmprojOffload: r.mmprojOffload,
        runtimeProfile: r.task,
      });

      setAppliedAt(new Date().toLocaleTimeString());
    } catch {
      // Config errors surface through the config surfaces elsewhere.
    } finally {
      setApplying(false);
    }
  }, [rec.task]);

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">RECOMMENDATION</span>
          <strong>{rec.summary}</strong>
        </div>
        <span className={`settings-chip chip-${rec.applied || appliedAt ? "good" : "neutral"}`}>
          {appliedAt ? `Applied ✓ ${appliedAt}` : rec.applied ? "✓ optimal" : "available"}
        </span>
      </div>

      {rec.reasons && rec.reasons.length > 0 ? (
        <details className="env-reasons">
          <summary>Why ({rec.reasons.length})</summary>

          <ul>
            {rec.reasons.map((reason, index) => (
              <li key={index}>{reason}</li>
            ))}
          </ul>
        </details>
      ) : null}

      {!rec.applied ? (
        <div className="header-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={() => void apply()}
            disabled={applying}
          >
            {applying ? "Applying…" : "Apply recommendations"}
          </button>

          <span className="runtime-hint">
            Predicted values — measure the effect afterwards in
            Settings → Performance → Live metrics.
          </span>
        </div>
      ) : null}
    </div>
  );
}

function HealthCard() {
  const [health, setHealth] = useState<HealthPayload | null>(null);

  const load = useCallback(() => {
    api
      .health()
      .then(setHealth)
      .catch(() => setHealth(null));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (!health) {
    return (
      <div className="settings-card">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">SYSTEM HEALTH</span>
            <strong>Verifying…</strong>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="settings-card">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">SYSTEM HEALTH</span>
          <strong>
            {health.overall === "ok"
              ? "All verified checks pass"
              : health.overall === "warn"
                ? "Verified, with warnings"
                : "Attention required"}
          </strong>
        </div>

        <div className="panel-heading-actions">
          <button type="button" className="text-button" onClick={load}>
            Refresh
          </button>

          <span className={`settings-chip chip-${HEALTH_TONE[health.overall] ?? "neutral"}`}>
            {health.checks.filter((c) => c.state === "ok").length}/{health.checks.length} ok
          </span>
        </div>
      </div>

      <div className="health-checks">
        {health.checks.map((check) => (
          <details key={check.id} className={`health-check state-${check.state}`}>
            <summary>
              <span className="health-check-label">
                <span
                  className={`health-check-symbol tone-${HEALTH_TONE[check.state] ?? "neutral"}`}
                  aria-hidden="true"
                >
                  {HEALTH_SYMBOL[check.state] ?? "?"}
                </span>
                {check.label}
              </span>

              <span className={`health-check-state tone-${HEALTH_TONE[check.state] ?? "neutral"}`}>
                {check.state}
              </span>
            </summary>

            <p className="health-check-evidence">{check.evidence}</p>
          </details>
        ))}
      </div>

      <span className="runtime-hint">
        Every state is evidence-backed — open any check to see the underlying
        proof. Green never means assumed.
      </span>
    </div>
  );
}

const SystemPanel = function SystemPanel() {
  const [env, setEnv] = useState<EnvironmentPayload | null>(null);

  useEffect(() => {
    let cancelled = false;

    api
      .environment()
      .then((payload) => {
        if (!cancelled) setEnv(payload);
      })
      .catch(() => {
        if (!cancelled) setEnv(null);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="system-panel">
      <DeviceCard env={env} />
      <RuntimeCard env={env} />
      <RecommendationCard env={env} />
      <HealthCard />
    </section>
  );
};

export default SystemPanel;
