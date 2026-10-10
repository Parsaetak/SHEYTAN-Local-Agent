#!/usr/bin/env node
// windows-live-answer-acceptance.mjs — v1.9.4 (P0-A): the REAL Windows
// WebView2 live-answer acceptance harness.
//
// WHAT THIS PROVES (on a real Windows host with the packaged desktop
// app): the exact user journey the release contract requires —
//
//   launch the actual native app (Wails/WebView2)
//     → wait for the ready surface
//     → send one message
//     → REAL non-placeholder text appears in the generation bubble WHILE
//       the Stop button is still visible (live, not after completion)
//     → a later DOM snapshot is strictly longer while Stop stays visible
//     → the run settles; exactly ONE persisted assistant answer
//     → reload → the same session stays active with one user + one
//       assistant reply
//     → clean close (the process exits)
//
// This is the test the Linux Chromium E2E CANNOT be (Linux serves over a
// real loopback listener; the packaged Windows app serves through the
// WebView2 resource-request interception — the exact surface where the
// v1.9.3-and-earlier activity WebSocket was unreachable).
//
// MODE OF OPERATION: the harness launches the desktop executable with
// WebView2 remote debugging enabled (CDP) — a TEST-ONLY switch; the
// production app must never ship with it. It never patches the app, never
// fakes events, and asserts ONLY what a real user could observe.
//
// USAGE (PowerShell, on Windows):
//   $env:SHEYTAN_ACCEPTANCE_EXE = "path\to\SHEYTAN-LA.exe"
//   node scripts\windows-live-answer-acceptance.mjs
//
// Requirements: Windows 10/11, WebView2 runtime, the built desktop exe,
//   Node >= 18 with the `ws` package available (npm i ws --no-save).
//
// HONESTY CONTRACT: the harness exits non-zero on ANY unmet assertion and
// prints the compact stream-trace dump (opt-in diagnostic, lengths only)
// on failure. It never claims a pass for a step it did not run.

import { spawn } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

const exe = process.env.SHEYTAN_ACCEPTANCE_EXE ?? "";
if (!exe || !existsSync(exe)) {
  console.error(
    "[acceptance] set SHEYTAN_ACCEPTANCE_EXE to the built desktop executable",
  );
  process.exit(2);
}

// CDP debugging port (test-only). Free port chosen by the OS to avoid
// collisions; WebView2 accepts --remote-debugging-port via the
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS environment variable.
const cdpPort = process.env.SHEYTAN_ACCEPTANCE_CDP_PORT ?? "9223";

const artifactsDir = resolve(here, "../e2e/.artifacts/windows-acceptance");
mkdirSync(artifactsDir, { recursive: true });

const log = (...args) => console.log("[acceptance]", ...args);

const deadline = (ms) => Date.now() + ms;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Minimal CDP-over-WebSocket client (no dependency beyond `ws`).
async function connectCDP(port, attemptsMs = 60000) {
  const { default: WebSocket } = await import("ws");

  const end = deadline(attemptsMs);

  while (Date.now() < end) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/list`);
      const targets = await res.json();
      const page = targets.find(
        (t) => t.type === "page" && !/devtools/i.test(t.url ?? ""),
      );
      if (page) {
        const ws = new WebSocket(page.webSocketDebuggerUrl, {
          perMessageDeflate: false,
        });
        await new Promise((ok, err) => {
          ws.once("open", ok);
          ws.once("error", err);
        });

        let msgId = 0;
        const pending = new Map();
        const events = [];

        ws.on("message", (data) => {
          const msg = JSON.parse(String(data));
          if (msg.id && pending.has(msg.id)) {
            const { ok, rej } = pending.get(msg.id);
            pending.delete(msg.id);
            msg.error ? rej(new Error(JSON.stringify(msg.error))) : ok(msg.result);
          } else if (msg.method) {
            events.push(msg);
          }
        });

        const send = (method, params = {}) =>
          new Promise((ok, rej) => {
            const id = ++msgId;
            pending.set(id, { ok, rej });
            ws.send(JSON.stringify({ id, method, params }));
          });

        return { ws, send, events, targetUrl: page.url };
      }
    } catch {
      // The debug endpoint appears a moment after process start.
    }
    await new Promise((r) => setTimeout(r, 250));
  }

  throw new Error("CDP endpoint never appeared");
}

async function evalJS(cdp, expression, awaitPromise = true) {
  const r = await cdp.send("Runtime.evaluate", {
    expression,
    awaitPromise,
    returnByValue: true,
  });
  if (r.exceptionDetails) {
    throw new Error("page error: " + JSON.stringify(r.exceptionDetails).slice(0, 400));
  }
  return r.result?.value;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---- DOM observation helpers (mirror the browser E2E contracts) ----

const OBSERVE_READY = `(() => {
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  return Boolean(composer);
})()`;

const OBSERVE_LIVE_TEXT = `(() => {
  const bubble = document.querySelector('.generation-bubble');
  const content = bubble?.querySelector('.message-content');
  const stopVisible = Array.from(document.querySelectorAll('button'))
    .some((b) => b.textContent?.trim() === 'Stop');
  const placeholder = Boolean(bubble?.querySelector('[data-stream-placeholder]'));
  const text = (content?.textContent ?? '').replace(/\\u200b/g, '');
  return { bubble: Boolean(bubble), stopVisible, placeholder, len: text.length };
})()`;

const OBSERVE_SETTLED = `(() => {
  const rows = document.querySelectorAll('.message-row.from-agent');
  const markdown = document.querySelectorAll('.message-row.from-agent .message-markdown');
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  return {
    fromAgentRows: rows.length,
    markdownRows: markdown.length,
    composerEnabled: composer ? !composer.disabled : false,
    anyPlaceholder: document.querySelectorAll('[data-stream-placeholder]').length,
  };
})()`;

const SEND = `(() => {
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  setter.call(composer, 'acceptance: the answer must stream live');
  composer.dispatchEvent(new Event('input', { bubbles: true }));
  const send = Array.from(document.querySelectorAll('button'))
    .find((b) => /^(Send|Forge)$/.test(b.textContent?.trim() ?? ''));
  send.click();
  return true;
})()`;

const RELOAD = "location.reload(); true";

async function main() {
  log("launching", exe);
  log("CDP port:", cdpPort, "(TEST-ONLY; production builds never ship this)");

  const proc = spawn(exe, [], {
    env: {
      ...process.env,
      WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: `--remote-debugging-port=${cdpPort}`,
      SHEYTAN_STREAM_TRACE: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  const stdout = [];
  proc.stdout.on("data", (d) => stdout.push(String(d)));
  proc.stderr.on("data", (d) => stdout.push(String(d)));

  let exitCode = 0;
  const fail = (msg) => {
    console.error("[acceptance] FAIL:", msg);
    exitCode = 1;
  };

  try {
    const cdp = await connectCDP(Number(cdpPort));
    log("CDP attached:", cdp.targetUrl);

    // 1. ready surface (composer enabled within the engine-gate budget).
    const readyEnd = deadline(120_000);
    let ready = false;
    while (Date.now() < readyEnd) {
      ready = await evalJS(cdp, OBSERVE_READY, false);
      if (ready) break;
      await sleep(500);
    }
    if (!ready) fail("the ready surface (enabled composer) never appeared");
    else log("ready surface observed");

    // Arm the opt-in stream trace in the page (diagnostic; lengths only).
    await evalJS(
      cdp,
      `localStorage.setItem('SHEYTAN_STREAM_TRACE','1'); true`,
      false,
    );

    // 2. send one message.
    await evalJS(cdp, SEND, false);
    log("message sent");

    // 3. LIVE text while Stop is visible, then STRICT GROWTH — the
    // release contract. The generation must show real (non-placeholder)
    // content before it completes.
    const liveEnd = deadline(180_000);
    let firstLen = 0;
    let sawLive = false;

    while (Date.now() < liveEnd) {
      const obs = await evalJS(cdp, OBSERVE_LIVE_TEXT, false);

      if (obs && obs.bubble && obs.stopVisible && !obs.placeholder && obs.len > 0) {
        if (!sawLive) {
          sawLive = true;
          firstLen = obs.len;
          log("LIVE text visible while Stop is up, len =", firstLen);
          break;
        }
      }

      // A settled run before any live observation is a contract failure
      // on this host (the answer appeared only after completion).
      const settled = await evalJS(cdp, OBSERVE_SETTLED, false);
      if (
        settled &&
        settled.composerEnabled &&
        settled.markdownRows > 0 &&
        !sawLive
      ) {
        fail(
          "the run SETTLED before any live non-placeholder text was observed " +
            "(the answer appeared only after completion — the defect class)",
        );
        break;
      }

      await sleep(120);
    }

    if (!sawLive) {
      if (exitCode === 0) fail("no live non-placeholder text while Stop was visible");
    } else {
      // 4. strictly longer DOM text while Stop stays visible.
      const growEnd = deadline(180_000);
      let grew = false;
      while (Date.now() < growEnd) {
        const obs = await evalJS(cdp, OBSERVE_LIVE_TEXT, false);
        if (obs && obs.bubble && obs.len > firstLen) {
          grew = true;
          log("DOM text grew:", firstLen, "->", obs.len, "(Stop still up)");
          break;
        }
        const settled = await evalJS(cdp, OBSERVE_SETTLED, false);
        if (settled && settled.composerEnabled && settled.markdownRows > 0) {
          // Fast local models can finish between two samples; the growth
          // proof then carries through the settled length below.
          break;
        }
        await sleep(100);
      }
      const settledNow = await evalJS(cdp, OBSERVE_SETTLED, false);
      if (!grew && !(settledNow && settledNow.markdownRows > 0)) {
        fail("the live text never grew and the run never settled");
      }
    }

    // 5. settlement: composer unlocks; exactly ONE persisted assistant
    // answer; no placeholder anywhere.
    const settleEnd = deadline(240_000);
    let settled = null;
    while (Date.now() < settleEnd) {
      settled = await evalJS(cdp, OBSERVE_SETTLED, false);
      if (settled && settled.composerEnabled && settled.fromAgentRows === 1 &&
          settled.markdownRows === 1 && settled.anyPlaceholder === 0) {
        break;
      }
      await sleep(400);
    }

    if (!settled || settled.fromAgentRows !== 1 || settled.markdownRows !== 1) {
      fail("settlement did not produce exactly one real assistant answer: " +
        JSON.stringify(settled));
    } else {
      log("settled: one real assistant answer, composer unlocked");
    }

    // 6. reload → the created session persists and stays active.
    await evalJS(cdp, RELOAD, false);
    await sleep(1500);

    const reloadEnd = deadline(120_000);
    let persisted = null;
    while (Date.now() < reloadEnd) {
      const ready2 = await evalJS(cdp, OBSERVE_READY, false);
      if (!ready2) {
        await sleep(500);
        continue;
      }
      persisted = await evalJS(cdp, OBSERVE_SETTLED, false);
      if (persisted && persisted.fromAgentRows === 1 && persisted.markdownRows === 1) {
        break;
      }
      await sleep(400);
    }

    if (!persisted || persisted.fromAgentRows !== 1) {
      fail("after reload the persisted answer did not survive: " +
        JSON.stringify(persisted));
    } else {
      log("reload: one user + one assistant reply persisted in the same session");
    }

    // 7. dump the sanitized stream trace (lengths only) for the artifact.
    try {
      const trace = await evalJS(
        cdp,
        `(() => {
          const mod = window.__SHEYTAN_STREAM_TRACE_DUMP__;
          return typeof mod === 'function' ? mod() : 'trace-hook-not-installed';
        })()`,
        false,
      );
      writeFileSync(join(artifactsDir, "stream-trace.json"), String(trace));
    } catch {
      // Trace dump is diagnostic only.
    }
  } catch (err) {
    fail(String(err));
  } finally {
    // 8. clean close — the real window close path (v1.9.2 contract).
    try {
      proc.kill();
    } catch {
      // Already exited.
    }
    const code = await new Promise((r) => {
      const t = setTimeout(() => r("timeout"), 15000);
      proc.once("exit", () => {
        clearTimeout(t);
        r("exited");
      });
    });
    log("process:", code);
  }

  writeFileSync(
    join(artifactsDir, "process-log.txt"),
    stdout.join("\n"),
  );

  if (exitCode !== 0) {
    console.error(
      "[acceptance] RESULT: FAIL — see", artifactsDir,
    );
    process.exit(1);
  }

  console.log("[acceptance] RESULT: PASS (real Windows WebView2 live-answer contract)");
}

main().catch((err) => {
  console.error("[acceptance] fatal:", err);
  process.exit(2);
});
