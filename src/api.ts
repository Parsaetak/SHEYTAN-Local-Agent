import { API_BASE } from "./config";

export interface AppState {
  appName: string;
  appVersion: string;
  state: string;
}

export interface SysInfoCPU {
  name: string;
  physicalCores: number;
  logicalCores: number;
  frequencyMHz: number;
}

export interface SysInfoRAM {
  totalBytes: number;
  freeBytes: number;
  availableBytes: number;
}

export interface SysInfoDisk {
  totalBytes: number;
  freeBytes: number;
  path: string;
}

export interface SysInfoGPU {
  vendor: string;
  name: string;
  vramBytes: number;
  driverVersion: string;
}

export interface SysInfoRecommended {
  numThread: number;
  numGPU: number;
  numCtx: number;
  numBatch: number;
  maxTokens: number;
  canRunCPU: boolean;
  canRunGPU: boolean;
  warnings: string[];
}

export interface SysInfo {
  os: string;
  arch: string;
  hostname: string;
  cpu: SysInfoCPU;
  ram: SysInfoRAM;
  disk: SysInfoDisk;
  gpus: SysInfoGPU[];
  wsl2: boolean;
  docker: boolean;
  recommended: SysInfoRecommended;
}

export interface Preset {
  id: string;
  name?: string;
  label?: string;
  description?: string;
  temperature?: number;
  top_p?: number;
  top_k?: number;
  max_tokens?: number;
  repeat_penalty?: number;
  mirostat?: number;
  num_ctx?: number;
}

// v1.2.0 — Environment Centre / health / recommendation / update payloads.
// Every field below is backend-measured or backend-derived; the UI renders
// unknown values as "—" instead of inventing them.

export interface HardwareCPU {
  name: string;
  physicalCores?: number;
  logicalCores?: number;
  frequencyMHz?: number;
}

export interface HardwareRAM {
  totalBytes: number;
  freeBytes?: number;
  availableBytes?: number;
}

export interface HardwareStorage {
  totalBytes?: number;
  freeBytes?: number;
  path?: string;
}

export interface HardwareGPU {
  vendor?: string;
  name?: string;
  vramBytes?: number;
  driverVer?: string;
}

export interface HardwareBackend {
  engineBinary?: string;
  vulkan: boolean;
  nativeEnabled: boolean;
  engineTag?: string;
}

export interface EnvironmentPayload {
  device: {
    os: string;
    arch: string;
    cpu: HardwareCPU;
    ram: HardwareRAM;
    storage: HardwareStorage;
    gpus: HardwareGPU[];
    backend: HardwareBackend;
    identity: {
      product: string;
      shortName: string;
      appUserModelId?: string;
      platform: string;
    };
  };
  runtime: {
    engineState: string;
    enginePhase?: string;
    provider: string;
    backend: string;
    model?: string;
    context?: number;
    verified: boolean;
    vision: {
      state: string;
      reason?: string;
      active: boolean;
      projector?: string;
      bytes?: number;
    };
    engineTag?: string;
  };
  recommendation: {
    task: string;
    applied: boolean;
    context?: number;
    threads?: number;
    gpuLayers?: number;
    summary: string;
    reasons?: string[];
  };
  generatedAt: string;
}

export interface HealthCheck {
  id: string;
  label: string;
  state: "ok" | "warn" | "fail" | "unknown" | "na";
  evidence: string;
}

export interface HealthPayload {
  overall: "ok" | "warn" | "fail";
  checks: HealthCheck[];
  generatedAt: string;
}

export interface TaskProfileInfo {
  id: string;
  label: string;
  description: string;
}

export interface RuntimeRecommendation {
  task: string;
  context: number;
  threads: number;
  threadsBatch: number;
  gpuLayers: number;
  ubatchSize: number;
  flashAttention: boolean;
  kvCacheQuant: string;
  gpuAutoOffload: boolean;
  mmprojOffload: string;
  projectorDevice: string;
  cacheReuse?: number;
  predicted: {
    totalBytes?: number;
    kvBytes?: number;
    class?: string;
    speed?: string;
    vramBytes?: number;
  };
  reasons: string[];
  notes?: string[];
}

export interface RecommendationPayload {
  task: string;
  current: {
    model?: string;
    context?: number;
    threads?: number;
    ubatchSize?: number;
    gpuLayers?: number;
    gpuAutoOffload: boolean;
    flashAttention: boolean;
    kvCacheQuant?: string;
    mmprojOffload?: string;
    runtimeProfile?: string;
  };
  recommended?: RuntimeRecommendation;
  available: TaskProfileInfo[];
}

export interface AppUpdateStatus {
  state:
    | "up-to-date"
    | "update-available"
    | "check-failed"
    | "downloading"
    | "ready"
    | "failed"
    | "unknown";
  version: string;
  latest?: string;
  channel?: string;
  notes?: string;
  publishedAt?: string;
  stagedPath?: string;
  stagedSHA256?: string;
  checkedAt?: string;
  message?: string;
  // v1.2.3: live staging progress from the Download Manager while the
  // state is "downloading" — phase, bytes, speed, ETA, source, checks.
  download?: DownloadProgress;
}

// v1.2.3 Download Manager progress (mirrors internal/downloader.Progress).
// `bytesTotal`/`etaSeconds` go negative when the server did not declare a
// size; the UI must render indeterminate states honestly.
export interface DownloadProgress {
  phase:
    | "resolving"
    | "connecting"
    | "downloading"
    | "verifying"
    | "installing"
    | "ready";
  sourceUrl?: string;
  sourceLabel?: string;
  sourceTrust?: string;
  attempt?: number;
  bytesDone: number;
  bytesTotal: number;
  bytesPerSec?: number;
  etaSeconds?: number;
  retries?: number;
  resumable?: boolean;
  verified?: boolean;
  paused?: boolean;
  message?: string;
}

export interface Model {
  id: string;
  name: string;
  provider?: string;
  path?: string;
  sizeBytes?: number;
  loaded?: boolean;
  // v1.1.5Z Phase 6: true when the ACTIVE backend is currently serving
  // this model (the honest "currently serving" marker).
  serving?: boolean;
  // v1.1.4Z: GGUF header metadata (populated from the model card parser).
  architecture?: string;
  quantization?: string;
  contextLength?: number;
  parameterInfo?: string;
  // v1.1.6 first-use model card: backend-measured footprint at the
  // model class' recommended context, plus that recommendation itself.
  estimatedMemoryBytes?: number;
  recommendedContext?: number;
  // v1.1.8 model-picker capability facts — measured by the backend's
  // capability resolver and served from its bounded cache. The UI never
  // invents these: a missing field means "not detected", not "maybe".
  multimodal?: boolean;
  nativeBackend?: boolean;
  chatTemplate?: boolean;
  nativeReason?: string;
  estimatedVRAMBytes?: number;
  // v1.2.0 vision readiness state machine + projector evidence. The UI
  // renders exactly what the backend derived from GGUF architecture,
  // projector discovery and (when serving) the verified engine boot —
  // never from filenames alone.
  visionState?: string;
  visionReason?: string;
  mmprojPath?: string;
  mmprojName?: string;
  mmprojSizeBytes?: number;
  mmprojVerified?: boolean;
}

export interface ModelsResponse {
  local: Model[];
  loaded: Model[];
  llamaRunning: boolean;
  // v1.1.5Z Phase 6: honest status — which backend serves generation
  // ("native" | "llama") and which model file it is serving right now.
  backend?: string;
  servingPath?: string;
}

export interface ToolInfo {
  name: string;
  // v1.1.7: concise one-line description for the Options UI.
  description?: string;
  // v1.1.7: the full model-facing operational spec (never rendered in the
  // main Options list — kept for tooltips/debugging surfaces).
  detail?: string;
  enabled?: boolean;
}

export interface LLMConfig {
  temperature: number;
  topP: number;
  topK: number;
  minP: number;
  maxTokens: number;
  stop: string;
  seed: number;
  repeatPenalty: number;
  repeatLastN: number;
  presencePenalty: number;
  frequencyPenalty: number;
  mirostat: number;
  mirostatTau: number;
  mirostatEta: number;
  numCtx: number;
  numBatch: number;
  numGpu: number;
  numThread: number;
  stream: boolean;
  preset: string;
}

export interface RuntimeConfig {
  dataDir: string;
  modelsDir: string;
  sessionsDir: string;

  host: string;
  port: number;

  llmBaseUrl: string;
  model: string;

  provider: string;
  remoteBaseUrl: string;
  remoteApiKey: string;
  remoteModel: string;

  llamaBinPath: string;
  llamaHost: string;
  llamaPort: number;
  llamaAutoStart: boolean;
  llamaExtraArgs: string;

  maxIterations: number;
  parallelTools: boolean;

  runTimeoutMinutes?: number;
  sandboxEnabled: boolean;
  sandboxMemory: string;
  sandboxCPU: number;

  llm: LLMConfig;

  browserExecutablePath: string;
  browserHeadless: boolean;
  browserSlowMoMs: number;

  updateSchedule: string;
  lastUpdateCheck: string;

  thinkingMode: boolean;
  enabledTools: string[];
  attachmentsBudgetKb: number;
  historyWindowPct: number;
  recallEnabled: boolean;
  recallTopK: number;

  gpuAutoOffload: boolean;
  engineCompat: number;
  // v1.1.7: why the engine last settled above compatibility level 0
  // (backend-managed; surfaced in Settings → Performance → Engine).
  engineCompatReason?: string;
  engineCompatAt?: string;
  flashAttention: boolean;
  cacheReuse: number;
  ubatchSize: number;
  threadsBatch: number;
  kvCacheQuant: string;
  mlock: boolean;
  draftModel: string;
  showPerfHud: boolean;

  visionEnabled: boolean;
  visionMmproj: string;
  // v1.2.0: projector GPU-offload posture (auto | on | off) and the
  // active task-aware runtime profile id (chat | coding | research |
  // vision | agent | low-power | maximum).
  visionMmprojOffload: string;
  runtimeProfile: string;

  maxWorkspaceMb: number;
  maxSessionsKept: number;
  maxLogMb: number;

  multiAgentDepth: number;

  continuumEnabled: boolean;
  continuumThresholdPct: number;
  continuumCarryMessages: number;
  continuumFrameworkTokens: number;

  smoothStream: boolean;
  targetFps: number;

  labEnabled: boolean;
  labWorkspaceRoot: string;
  labCommandTimeoutSec: number;
  labMaxIterations: number;
  labKeepWorkspaces: boolean;
  labAllowNetwork: boolean;

  researchEnabled: boolean;
  researchBackend: string;
  researchSearxngUrl: string;
  researchMaxResults: number;
  researchTimeoutSec: number;
  researchCacheTtlMin: number;
  researchGitHub: boolean;
  researchReddit: boolean;
  researchWeb: boolean;
  researchUserAgent: string;
}

export interface Session {
  id: string;
  title: string;
  model?: string;
  preset?: string;
  createdAt: string;
  updatedAt: string;
  threadId?: string;
  parentId?: string;
  chapter?: number;
  msgCount?: number;
}

export interface RunRequest {
  sessionId: string;
  message: string;
  attachmentIds?: string[];
  regenerate?: boolean;

  // v1.2.5 per-request controls — these change the actual backend
  // request (tier posture + tool surface), not just the UI.
  /** "auto" | "fast" | "thinking" */
  thinking?: string;
  /** "auto" | "manual" */
  toolMode?: string;
  /** manual-mode allow-list (tool names); ignored in auto mode */
  toolAllow?: string[];
}

export interface RunResponse {
  ok: boolean;
  sessionId: string;
  runId?: string;
  status?: string;
}

export interface AbortResponse {
  ok: boolean;
  status?: string;
}

export type ActivityEvent = {
  id?: string;
  type: string;
  caption?: string;
  timestamp?: string | number;
  sessionId?: string;
  data?: unknown;
  [key: string]: unknown;
};

export interface LabWorkspace {
  id: string;
  source: string;
  path: string;
  createdAt: string;
}

export interface LabCommand {
  command: string;
  workingDir?: string;
  timeout?: number;
  maxOutputBytes?: number;
}

export interface LabCommandResult {
  command: string;
  workingDir: string;
  stdout: string;
  stderr: string;
  output: string;
  exitCode: number;
  duration: number;
  startedAt: string;
  finishedAt: string;
  timedOut: boolean;
  canceled: boolean;
  outputLimit: boolean;
  success: boolean;
}

export type LabVerificationStatus =
  | "passed"
  | "failed"
  | "canceled"
  | "skipped";

export interface LabVerificationResult {
  name: string;
  status: LabVerificationStatus;
  result: LabCommandResult;
  error?: string;
  startedAt: string;
  finishedAt: string;
}

export interface LabVerificationSummary {
  passed: boolean;
  requiredTotal: number;
  requiredPassed: number;
  requiredFailed: number;
  optionalTotal: number;
  optionalPassed: number;
  optionalFailed: number;
  results: LabVerificationResult[];
  duration: number;
  startedAt: string;
  finishedAt: string;
  error?: string;
}

export type LabTaskStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"
  | "blocked";

export interface LabTaskSnapshot {
  id: string;
  title: string;
  description: string;
  status: LabTaskStatus;
  workspace?: LabWorkspace;
  commands?: LabCommand[];
  results?: LabCommandResult[];
  metadata?: Record<string, string>;
  lastVerification?: LabVerificationSummary;
  verificationPassed: boolean;
  verifiedAt?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
}

export interface LabTaskSessionSnapshot {
  id: string;
  createdAt: string;
  updatedAt: string;
  task: LabTaskSnapshot;
}

export interface LabListResponse {
  tasks: LabTaskSessionSnapshot[];
}

export interface LabActionResponse {
  ok: boolean;
  result?: unknown;
  error?: string;
}

export interface ResearchResult {
  title: string;
  url: string;
  snippet?: string;
  source: string;
  provider: string;
  publishedAt?: string;
  authority: string;
  matchScore: number;
  contentHash?: string;
  metadata?: Record<string, unknown>;
}

export interface ResearchResponse {
  ok: boolean;
  provider: string;
  query: string;
  duration: number;
  results: ResearchResult[];
  error?: string;
  providers?: string[];
  backend?: string;
}

export interface ResearchConfig {
  backend: string;
  providers: string[];
}

// v1.1.3Z: authoritative engine state (backend process state is the single
// source of truth; the UI never invents these).
export type EngineState =
  | "idle"
  | "downloading"
  | "starting"
  | "ready"
  | "running"
  | "busy"
  | "stopping"
  | "stopped"
  | "failed"
  | "remote";

export interface EngineSnapshot {
  state: EngineState;
  detail?: string;
  model?: string;
  loadedPath?: string;
  pid?: number;
  vision: boolean;
  provider: string;
  // v1.1.5Z: the backend that serves generation ("llama" in Phase 1 —
  // the native engine reports generation-incapable and selection falls
  // back). Informational; the badge keeps reading `state`.
  backend: string;
  // v1.1.5Z: supervised native engine status when the native path is
  // enabled (engineBackend: "native"). Absent by default. Local status
  // reads only — the poll never performs engine IPC.
  native?: NativeEngineStatus;
  // v1.1.6 startup flow: user-facing phase (waiting | downloading-engine |
  // loading-model | checking-capabilities | preparing-context | ready |
  // stopping | stopped | failed) and the verified-readiness proof —
  // "ready" is only reported when the serving model is verified.
  phase?: string;
  verified?: boolean;
  verifiedModel?: string;
  verifiedContext?: number;
  degraded?: boolean;
  // v1.2.0 runtime vision readiness block (loading | ready | degraded |
  // failed | projector-found | projector-missing | supported |
  // unsupported) with the evidence reason and measured projector size.
  visionState?: string;
  visionReason?: string;
  visionProjector?: string;
  visionProjectorName?: string;
  visionProjectorBytes?: number;
  visionActive?: boolean;
  // v1.2.3: live asset-download progress (llama.cpp archive, model
  // packages) from the Download Manager. Present only mid-download.
  download?: DownloadProgress;
  logs?: string[];
  cacheStats?: {
    entries: number;
    bytes: number;
    maxBytes: number;
    hits: number;
    misses: number;
    evictions: number;
    hitRatio: number;
  };
  timestamp: string;
}

// v1.1.5Z Phase 1: native engine status block. The state vocabulary is
// the same engine state union above; the UI badge must keep following
// the llama.cpp snapshot state until the native engine serves generation.
export interface NativeEngineStatus {
  selected: boolean;
  available: boolean;
  path?: string;
  state: EngineState;
  detail?: string;
  pid?: number;
  uptimeSeconds?: number;
  restarts?: number;
}

// v1.1.3Z: staged attachment metadata returned by the backend.
export interface AttachmentChunk {
  id: string;
  attId: string;
  index: number;
  hash: string;
  offset: number;
  bytes: number;
  tokens: number;
  preview: string;
}

export interface Attachment {
  id: string;
  name: string;
  kind: "text" | "image" | "binary";
  size: number;
  sha256: string;
  createdAt: string;
  chunks?: AttachmentChunk[];
  note?: string;
  sessionIds?: string[];
}

export interface UploadResponse {
  ok: boolean;
  attachments: Attachment[];
  failed: { name: string; error: string }[];
}

// v1.1.3Z: full session payload with persisted conversation history.
export interface SessionDetail extends Session {
  messages?: ChatMessage[];
  context?: {
    systemPrompt?: string;
    attachedFiles?: string[];
    attachmentIds?: string[];
    maxIterations?: number;
    // v1.1.6: per-session context-window policy in tokens
    // (0/absent = inherit the global configured context).
    contextTokens?: number;
  };
}

// v1.1.6: the backend-resolved context decision for one session. The
// backend is authoritative — the UI never computes a different truth.
export interface SessionContextOption {
  tokens: number;
  label: string;
  classification: "safe" | "caution" | "unsupported";
  available: boolean;
  reason?: string;
  modelClamped?: boolean;
  engineClamped?: boolean;
}

export interface SessionContextStatus {
  sessionId: string;
  requested: number;
  configured: number;
  sessionPolicy?: number;
  effective: number;
  modelMax?: number;
  engineMax?: number;
  usableInput: number;
  outputReserve: number;
  safetyReserve: number;
  used: number;
  remaining: number;
  pressure: number;
  classification: "safe" | "caution" | "unsupported";
  resourceReason?: string;
  reasons?: string[];
  options: SessionContextOption[];
}

export interface ChatMessage {
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  reasoning?: string;
  attachments?: string[];
  images?: string[];
  feedback?: number;
  at?: string;
}

// v1.1.7 — live performance, log viewer and connection diagnostics.

export interface CompatInfo {
  level: number;
  name: string;
  optimised: boolean;
  reason?: string;
  recordedAt?: string;
  changes?: string[];
}

export interface EnginePerfAPI {
  hasSample: boolean;
  genTokPerSec?: number;
  promptTokPerSec?: number;
  ttftSeconds?: number;
  promptTokens?: number;
  tokens?: number;
  samplesAvailable: number;
}

export interface PerfSnapshot {
  generatedAt: string;
  cpuPercent?: number | null;
  gpuPercent?: number | null;
  ram?: {
    totalBytes: number;
    availableBytes: number;
    usedBytes: number;
    usedPercent: number;
  } | null;
  vram?: {
    totalMb: number;
    usedMb: number;
    percent?: number;
    source: string;
  } | null;
  backend: string;
  engineState: string;
  model?: string;
  compat: CompatInfo;
  generation: EnginePerfAPI;
  context?: {
    usedTokens: number;
    total: number;
    percent?: number;
    source?: string;
  } | null;
  recommended?: RecommendedSettings | null;

  // v1.2.5: per-request measured timelines (Advanced/System diagnostics).
  requests?: RequestTimingEntry[];
  /** the data-ownership ladder (ACTIVE/SESSION/HOT/COLD/EXPIRED/RELEASE) */
  ownership?: OwnershipLevelEntry[];
  toolCache?: {
    resultHits: number;
    resultMisses: number;
    resultEntries: number;
    resultBytes: number;
  } | null;
}

/** One recent turn's measured timeline (v1.2.5). */
export interface RequestTimingEntry {
  at: string;
  sessionId?: string;
  tier?: string;
  finalTier?: string;
  escalations?: number;
  thinkingControl?: string;
  toolPolicyMode?: string;
  firstPromptTokens?: number;
  classifyMs?: number;
  contextMs?: number;
  promptMs?: number;
  serializationMs?: number;
  ttftMs?: number;
  generationMs?: number;
  toolMs?: number;
  verificationMs?: number;
  totalMs?: number;
  toolCalls?: number;
  verified?: string;
}

/** One ownership-ladder row (v1.2.5). */
export interface OwnershipLevelEntry {
  level: string;
  owners?: number;
  names?: string[];
  bytes?: number;
  at?: string;
  description: string;
}

export interface RecommendedSettings {
  threads: number;
  threadsBatch: number;
  gpuLayers: number;
  gpuAutoOffload: boolean;
  flashAttention: boolean;
  flashAttnState: string;
  ubatchSize: number;
  cacheReuse: number;
  context: number;
  notes?: string[];
}

// v1.2.4: Workspace work-environment surface.
export interface WorkspaceFileEntry {
  name: string;
  size: number;
  modTime: string;
}

export interface WorkspaceProjectFacts {
  root: string;
  languages?: string[];
  buildCmd?: string;
  testCmd?: string;
  lintCmd?: string;
  layout?: string;
  entryHint?: string;
  conventions?: string[];
  lessons?: string[];
  buildVerifiedAt?: string;
  testVerifiedAt?: string;
  lastObservedAt?: string;
}

export interface WorkspaceSummary {
  root: string;
  isDefault: boolean;
  exists: boolean;
  recentFiles?: WorkspaceFileEntry[];
  project?: WorkspaceProjectFacts | null;
  projectCard?: string;
  recentWorkspaces?: string[];
  activeSession?: {
    id: string;
    title?: string;
    model?: string;
    messages: number;
    updatedAt?: string;
  } | null;
  runtime?: {
    backend?: string;
    engineState?: string;
    model?: string;
  } | null;
  agent?: {
    running: boolean;
    state?: string;
  } | null;
}

export interface WorkspaceSwitchResult {
  ok: boolean;
  previous?: string;
  summary: WorkspaceSummary;
  changed?: string[];
}

export interface LogEntry {
  index: number;
  time: string;
  level: string;
  category: string;
  message: string;
  raw: string;
}

export interface LogsResponse {
  entries: LogEntry[];
  limit: number;
}

export type NetDiagState =
  | "Excellent"
  | "Good"
  | "Unstable"
  | "Slow"
  | "Offline";

export interface NetDiagResult {
  state: NetDiagState;
  reason?: string;
  latencyMs?: number;
  latency2Ms?: number;
  dnsOk: boolean;
  httpsOk: boolean;
  endpointOk: boolean;
  checkedAt: string;
  totalMs: number;
}

const DEFAULT_TIMEOUT_MS = 15_000;
const LONG_OPERATION_TIMEOUT_MS = 5 * 60_000;
const RESEARCH_TIMEOUT_MS = 30_000;
const uploadTimeoutMs = 2 * 60_000;

async function request<T>(
  path: string,
  init?: RequestInit,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  const controller = new AbortController();
  let timeoutTriggered = false;

  const timeout = window.setTimeout(() => {
    timeoutTriggered = true;
    controller.abort();
  }, timeoutMs);

  const callerSignal = init?.signal;

  const abortFromCaller = () => {
    controller.abort();
  };

  if (callerSignal) {
    if (callerSignal.aborted) {
      controller.abort();
    } else {
      callerSignal.addEventListener("abort", abortFromCaller, { once: true });
    }
  }

  try {
    const response = await fetch(`${API_BASE}${path}`, {
      ...init,
      signal: controller.signal,
      headers: {
        Accept: "application/json",
        ...(init?.body
          ? {
              "Content-Type": "application/json",
            }
          : {}),
        ...init?.headers,
      },
    });

    if (!response.ok) {
      const body = await response.text();
      let message = body;

      try {
        const parsed = JSON.parse(body) as {
          error?: string;
        };

        if (typeof parsed.error === "string" && parsed.error.trim()) {
          message = parsed.error;
        }
      } catch {
        // Preserve raw response text.
      }

      throw new Error(message || `Request failed with HTTP ${response.status}`);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  } catch (error) {
    if (timeoutTriggered) {
      throw new Error(
        `Request timed out after ${Math.round(timeoutMs / 1000)} seconds.`,
      );
    }

    throw error;
  } finally {
    window.clearTimeout(timeout);

    if (callerSignal) {
      callerSignal.removeEventListener("abort", abortFromCaller);
    }
  }
}

export const api = {
  state(): Promise<AppState> {
    return request<AppState>("/state");
  },

  sysinfo(): Promise<SysInfo> {
    return request<SysInfo>("/sysinfo");
  },

  presets(): Promise<Preset[]> {
    return request<Preset[]>("/presets");
  },

  models(): Promise<ModelsResponse> {
    return request<ModelsResponse>("/models");
  },

  tools(): Promise<ToolInfo[]> {
    return request<ToolInfo[]>("/tools");
  },

  config(): Promise<RuntimeConfig> {
    return request<RuntimeConfig>("/config");
  },

  updateConfig(payload: Record<string, unknown>): Promise<RuntimeConfig> {
    return request<RuntimeConfig>("/config", {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  llama(
    action: "start" | "stop" | "cancel-download",
  ): Promise<unknown> {
    return request<unknown>("/llama", {
      method: "POST",
      body: JSON.stringify({ action }),
    }, action === "cancel-download" ? 15_000 : LONG_OPERATION_TIMEOUT_MS);
  },

  sessions(): Promise<Session[]> {
    return request<Session[]>("/sessions");
  },

  session(id: string): Promise<Session> {
    return request<Session>(`/sessions/${encodeURIComponent(id)}`);
  },

  createSession(): Promise<Session> {
    return request<Session>("/sessions", {
      method: "POST",
    });
  },

  deleteSession(id: string): Promise<void> {
    return request<void>(`/sessions/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  run(payload: RunRequest): Promise<RunResponse> {
    return request<RunResponse>(
      "/run",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  // v1.1.3Z: authoritative engine snapshot (poll target).
  engine(): Promise<EngineSnapshot> {
    return request<EngineSnapshot>("/engine");
  },

  // v1.1.6: the backend-resolved context status for one session
  // (selector options, usage, pressure, resource verdict).
  sessionContext(id: string): Promise<SessionContextStatus> {
    return request<SessionContextStatus>(
      `/sessions/${encodeURIComponent(id)}/context`,
    );
  },

  // v1.1.6: set the per-session context policy (tokens; 0 = inherit the
  // global context). The choice travels with THIS session only.
  setSessionContext(
    id: string,
    contextTokens: number,
  ): Promise<SessionContextStatus> {
    return request<SessionContextStatus>(
      `/sessions/${encodeURIComponent(id)}/context`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ contextTokens }),
      },
    );
  },

  // v1.1.6 first-use: open the models directory in the OS file manager.
  openModelsFolder(): Promise<{ ok: boolean; path?: string }> {
    return request<{ ok: boolean; path?: string }>("/models/open-folder", {
      method: "POST",
    });
  },

  // v1.1.3Z: staged attachments.
  attachments(): Promise<{ attachments: Attachment[]; limits?: unknown }> {
    return request("/attachments");
  },

  uploadAttachments(
    sessionId: string,
    files: File[],
  ): Promise<UploadResponse> {
    const form = new FormData();

    for (const file of files) {
      form.append("files", file);
    }

    const controller = new AbortController();

    const timeout = window.setTimeout(() => {
      controller.abort();
    }, uploadTimeoutMs);

    return fetch(`${API_BASE}/attachments?sessionId=${encodeURIComponent(sessionId)}`, {
      method: "POST",
      body: form,
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          const text = await response.text();

          let message = text;

          try {
            const parsed = JSON.parse(text) as { error?: string };

            if (parsed.error) {
              message = parsed.error;
            }
          } catch {
            // Raw body.
          }

          throw new Error(message || `Upload failed (HTTP ${response.status})`);
        }

        return (await response.json()) as UploadResponse;
      })
      .finally(() => {
        window.clearTimeout(timeout);
      });
  },

  deleteAttachment(id: string): Promise<{ ok: boolean }> {
    return request(`/attachments/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  sessionDetail(id: string): Promise<SessionDetail> {
    return request<SessionDetail>(
      `/sessions/${encodeURIComponent(id)}`,
      undefined,
      DEFAULT_TIMEOUT_MS,
    );
  },

  // v1.1.4Z: recall feedback — 👍/👎 on a past exchange steers future
  // recall scoring (the backend sidecar existed since v1.0.6 with no
  // writer).
  feedback(payload: {
    sessionId: string;
    query: string;
    liked: boolean;
    clear?: boolean;
  }): Promise<void> {
    return request<void>("/feedback", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  abort(sessionId: string): Promise<AbortResponse> {
    return request<AbortResponse>("/abort", {
      method: "POST",
      body: JSON.stringify({
        sessionId,
      }),
    });
  },

  updateSession(
    id: string,
    payload: {
      title?: string;
      model?: string;
      context?: {
        systemPrompt?: string;
        attachedFiles?: string[];
        maxIterations?: number;
        contextTokens?: number;
      };
    },
  ): Promise<Session> {
    return request<Session>(`/sessions/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  // v1.1.7: compact live performance snapshot (real measurements only;
  // unavailable values arrive null and render as N/A).
  perf(): Promise<PerfSnapshot> {
    return request<PerfSnapshot>("/perf");
  },

  // v1.2.4: Workspace — one compact work-environment summary.
  workspace(): Promise<WorkspaceSummary> {
    return request<WorkspaceSummary>("/workspace");
  },

  // v1.2.4: reveal the workspace root in the OS file manager / terminal.
  workspaceReveal(target: "files" | "terminal" = "files"): Promise<{ ok: boolean; path: string }> {
    return request("/workspace/reveal", {
      method: "POST",
      body: JSON.stringify({ target }),
    });
  },

  // v1.2.4: switch the current project folder (refused while a run is
  // active; the response states exactly what changed).
  workspaceSwitch(path: string): Promise<WorkspaceSwitchResult> {
    return request("/workspace/switch", {
      method: "POST",
      body: JSON.stringify({ path }),
    });
  },

  // v1.1.7: recent, parsed, REDACTED app-log entries (bounded server-side).
  logs(limit = 512): Promise<LogsResponse> {
    return request<LogsResponse>(`/logs?limit=${limit}`);
  },

  // v1.1.7: one bounded connection diagnosis (no retries, never a gate —
  // SHEYTAN is local-first).
  netcheck(): Promise<NetDiagResult> {
    return request<NetDiagResult>("/netcheck", undefined, 20_000);
  },

  // v1.2.0: Environment Centre — the unified device/runtime/recommendation
  // view over existing telemetry. v1.2.2: accepts an AbortSignal so the
  // System tab can cancel its probes on navigation.
  environment(signal?: AbortSignal): Promise<EnvironmentPayload> {
    return request<EnvironmentPayload>("/environment", { signal }, 20_000);
  },

  // v1.2.0: verified health — one check per subsystem WITH evidence.
  // v1.2.2: accepts an AbortSignal (cancellable refresh).
  health(signal?: AbortSignal): Promise<HealthPayload> {
    return request<HealthPayload>("/health", { signal }, 20_000);
  },

  // v1.2.0: evidence-based runtime recommendation for a model + task
  // profile (Detected → Calculated → Recommended; applying stays explicit).
  recommendation(model = "", task = ""): Promise<RecommendationPayload> {
    const q = new URLSearchParams();
    if (model) q.set("model", model);
    if (task) q.set("task", task);
    const qs = q.toString();
    return request<RecommendationPayload>(
      `/recommendation${qs ? `?${qs}` : ""}`,
      undefined,
      30_000,
    );
  },

  // v1.2.0: application update surface (manifest-verified).
  updateStatus(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>("/update/status");
  },

  updateCheck(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>("/update/check", { method: "POST" }, 60_000);
  },

  updateDownload(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>(
      "/update/download",
      { method: "POST" },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  // v1.2.3: stop the running staging download immediately. The .part
  // file stays on disk so a retry resumes instead of restarting.
  updateCancel(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>("/update/cancel", { method: "POST" });
  },

  lab(): Promise<LabListResponse> {
    return request<LabListResponse>("/lab");
  },

  labTask(id: string): Promise<LabTaskSessionSnapshot> {
    return request<LabTaskSessionSnapshot>(
      `/lab/${encodeURIComponent(id)}`,
    );
  },

  labAction(payload: Record<string, unknown>): Promise<LabActionResponse> {
    return request<LabActionResponse>(
      "/lab",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  researchConfig(): Promise<ResearchConfig> {
    return request<ResearchConfig>("/research");
  },

  research(payload: {
    query: string;
    backend?: string;
    maxResults?: number;
    timeoutSec?: number;
  }): Promise<ResearchResponse> {
    return request<ResearchResponse>(
      "/research",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      RESEARCH_TIMEOUT_MS,
    );
  },
};

