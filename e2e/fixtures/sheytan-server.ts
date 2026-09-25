import { spawn, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { request } from "@playwright/test";

/**
 * sheytan-server.ts — the REAL application stack for browser E2E.
 *
 * Everything is required and fails loudly when missing (required-gate
 * semantics: a missing dependency must NEVER become a silent pass):
 *
 *   e2e/.build/sheytan-e2e-server        — go build -tags headless of this tree
 *   web/static/index.html                — npm run build (the embedded frontend)
 *   native/engine/build/shtn-engine-host — the real C++ engine (CMake)
 *   python3 + e2e/make-e2e-model.py      — the wide-context fixture generator
 *
 * The server runs against an ISOLATED per-run data directory (a temp
 * dir under e2e/.artifacts), configured with engineBackend=native and
 * the fixture model — the same engine selection a user makes in
 * Settings, so generation is REAL (real logits, real streaming, real
 * persistence), not a mocked SSE stream.
 */

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.resolve(HERE, "..", "..");
export const SERVER_BIN = path.join(REPO_ROOT, "e2e", ".build", "sheytan-e2e-server");
export const ENGINE_BIN = (() => {
  const name = process.platform === "win32" ? "shtn-engine-host.exe" : "shtn-engine-host";
  return path.join(REPO_ROOT, "native", "engine", "build", name);
})();

export interface SheytanStack {
  baseURL: string;
  dataDir: string;
  pid: number;
  child?: ChildProcess;
  log(): string;
  stop(): Promise<void>;
}

function requireFile(label: string, file: string): void {
  if (!fs.existsSync(file)) {
    throw new Error(
      `[e2e] missing required ${label}: ${file}\n` +
        (label === "server binary"
          ? "  build it first:  npm run build:e2e\n"
          : label === "embedded frontend"
            ? "  build it first:  npm run build\n"
            : "  build it first:  cmake -S native/engine -B native/engine/build && cmake --build native/engine/build\n") +
        "  The browser suite proves the REAL stack; it never runs green without it.",
    );
  }
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (addr && typeof addr === "object") {
        const port = addr.port;
        srv.close(() => resolve(port));
        return;
      }
      reject(new Error("no free port"));
    });
  });
}

async function waitForHealth(baseURL: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr = "";

  while (Date.now() < deadline) {
    try {
      const ctx = await request.newContext({ baseURL });
      const res = await ctx.get("/api/health");
      const body = (await res.json()) as { overall?: string };
      await ctx.dispose();
      if (res.ok() && body.overall === "ok") {
        return;
      }
      lastErr = `health=${res.status()} overall=${body.overall}`;
    } catch (err) {
      lastErr = String(err);
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  throw new Error(`[e2e] the real server never became healthy: ${lastErr}`);
}

export async function startSheytan(): Promise<SheytanStack> {
  requireFile("server binary", SERVER_BIN);
  requireFile("embedded frontend", path.join(REPO_ROOT, "web", "static", "index.html"));
  requireFile("native engine host", ENGINE_BIN);

  const artifacts = path.join(REPO_ROOT, "e2e", ".artifacts");
  fs.mkdirSync(artifacts, { recursive: true });

  const dataDir = fs.mkdtempSync(path.join(artifacts, "stack-"));
  const modelsDir = path.join(dataDir, "models");
  const binDir = path.join(dataDir, "bin");
  fs.mkdirSync(modelsDir, { recursive: true });
  fs.mkdirSync(binDir, { recursive: true });

  // The wide-context fixture model (real llama graph, ctx=4096).
  const modelPath = path.join(modelsDir, "e2e-wide.gguf");
  const gen = spawn(process.platform === "win32" ? "python" : "python3", [
    path.join(REPO_ROOT, "e2e", "make-e2e-model.py"),
    modelPath,
  ]);
  const genCode: number = await new Promise((resolve) => {
    gen.on("error", () => resolve(-1));
    gen.on("exit", (code) => resolve(code ?? -1));
  });
  if (genCode !== 0 || !fs.existsSync(modelPath)) {
    throw new Error("[e2e] the fixture model could not be generated (python3 required)");
  }

  fs.copyFileSync(ENGINE_BIN, path.join(binDir, path.basename(ENGINE_BIN)));

  const port = await freePort();

  fs.writeFileSync(
    path.join(dataDir, "config.json"),
    JSON.stringify(
      {
        dataDir,
        modelsDir,
        sessionsDir: path.join(dataDir, "sessions"),
        host: "127.0.0.1",
        port,
        provider: "local",
        model: "e2e-wide.gguf",
        engineBackend: "native",
        nativeEnginePath: path.join(binDir, path.basename(ENGINE_BIN)),
        llamaHost: "127.0.0.1",
        llamaPort: port + 1,
        llamaAutoStart: true,
      },
      null,
      2,
    ),
  );

  const child: ChildProcess = spawn(SERVER_BIN, ["serve", "--port", String(port), "--no-browser"], {
    env: { ...process.env, SHEYTAN_DATA_DIR: dataDir },
    stdio: ["ignore", "pipe", "pipe"],
    // Detached process group so the WHOLE tree (server + engine host)
    // can be terminated together.
    detached: process.platform !== "win32",
  });

  const serverLog: string[] = [];
  child.stdout?.on("data", (d) => serverLog.push(String(d)));
  child.stderr?.on("data", (d) => serverLog.push(String(d)));

  const exited = new Promise<never>((_, reject) => {
    child.on("exit", (code) =>
      reject(
        new Error(
          `[e2e] the real server exited during startup (code ${code}):\n${serverLog.join("").slice(-4000)}`,
        ),
      ),
    );
  });

  const baseURL = `http://127.0.0.1:${port}`;

  try {
    await Promise.race([waitForHealth(baseURL, 90_000), exited]);
  } catch (err) {
    stopChild(child);
    throw err;
  }

  return {
    baseURL,
    dataDir,
    pid: child.pid ?? 0,
    child,
    log: () => serverLog.join(""),
    stop: async () => {
      stopChild(child);
      await new Promise((r) => setTimeout(r, 400));
    },
  };
}

function stopChild(child: ChildProcess): void {
  if (child.exitCode !== null) {
    return;
  }
  try {
    if (child.pid && process.platform !== "win32") {
      // The server owns its engine subprocesses and tears them down on
      // SIGTERM (Close(): native first, then llama.cpp) — a graceful
      // stop keeps the isolated data dir consistent.
      process.kill(-child.pid, "SIGTERM");
    } else {
      child.kill("SIGTERM");
    }
  } catch {
    try {
      child.kill("SIGKILL");
    } catch {
      /* already gone */
    }
  }
}
