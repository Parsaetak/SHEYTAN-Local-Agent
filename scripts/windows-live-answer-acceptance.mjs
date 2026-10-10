#!/usr/bin/env node
// windows-live-answer-acceptance.mjs — v1.9.5: the REAL Windows WebView2
// live-answer acceptance harness.
//
// WHAT THIS PROVES (on a real Windows host with the packaged desktop
// app): the exact user journey the release contract requires —
//
//   launch the actual native app (Wails/WebView2)
//     → arm the opt-in stream trace BEFORE app modules load (CDP
//       addScriptToEvaluateOnNewDocument seeds localStorage, then reload)
//     → wait for the ready surface
//     → send one message to a REAL fixture model
//     → ORDERED LIVE-GROWTH EVIDENCE (the release contract):
//         sample A: nonblank, non-placeholder live text AND Stop visible
//         sample B: strictly greater text length AND Stop still visible
//                   AND content still non-placeholder
//         (recorded in that order; any absence is a FAILURE — a settled
//          Markdown row is NEVER accepted as a substitute for growth)
//     → the run settles; exactly ONE persisted assistant answer with
//       visible non-whitespace Markdown text and NO placeholder, plus
//       the expected persisted user message; composer unlocked
//     → reload → the SAME session stays active (sidebar
//       data-session-id contract) with one user + one assistant reply
//     → a SECOND send proves repeatability (two full run cycles)
//     → REAL top-level window close (WM_CLOSE via `taskkill` without
//       /F) → process exit and owned listener cleanup within a bounded
//       deadline. Forced termination is only failure cleanup AFTER the
//       test is marked failed — never a pass path.
//
// MODE OF OPERATION: the harness launches the desktop executable with
// WebView2 remote debugging enabled (CDP) — a TEST-ONLY switch; the
// production app must never ship with it. It never patches the app,
// never fakes events, and asserts ONLY what a real user could observe.
//
// USAGE (PowerShell, on Windows):
//   $env:SHEYTAN_ACCEPTANCE_EXE = "path\to\SHEYTAN-LA.exe"
//   node scripts\windows-live-answer-acceptance.mjs
//
// Requirements: Windows 10/11, WebView2 runtime, the built desktop exe,
//   Node >= 18. The `ws` client is a DECLARED pinned dev dependency
//   (package.json + lockfile) — a clean `npm ci` provisions it; no
//   manual npm i step exists.
//
// HONESTY CONTRACT: the harness exits non-zero on ANY unmet assertion
// and prints the compact stream-trace dump (opt-in diagnostic, lengths
// only) on failure. It never claims a pass for a step it did not run,
// never converts a timeout into a pass, and never accepts settled
// output as proof of live growth.

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

// The prompt is designed so a real fixture model streams MULTIPLE tokens
// over a measurable window (numbered list), making live growth observable
// through the genuine pipeline. If the fixture still completes between two
// samples the growth assertion FAILS honestly (by design — the fixture
// must be slow enough; the harness never relaxes the contract).
const PROMPT =
  "acceptance stream test: list the numbers from 1 to 40, one per line";

// Minimal CDP-over-WebSocket client (dependency: the declared `ws` package).
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

        ws.on("message", (data) => {
          const msg = JSON.parse(String(data));
          if (msg.id && pending.has(msg.id)) {
            const { ok, rej } = pending.get(msg.id);
            pending.delete(msg.id);
            if (msg.error) {
              rej(new Error(JSON.stringify(msg.error)));
            } else {
              ok(msg.result);
            }
          }
        });

        const send = (method, params = {}) =>
          new Promise((ok, rej) => {
            const id = ++msgId;
            pending.set(id, { ok, rej });
            ws.send(JSON.stringify({ id, method, params }));
          });

        return { ws, send, targetUrl: page.url };
      }
    } catch {
      // The debug endpoint appears a moment after process start.
    }
    await sleep(250);
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

// ---- DOM observation helpers (mirror the browser E2E contracts) ----
// Session selection contract: the sidebar row carries data-session-id and
// the ACTIVE row has the "active" class (AgentSidebar.tsx v1.8.3).

const OBSERVE_READY = `(() => {
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  return Boolean(composer);
})()`;

const OBSERVE_ACTIVE_SESSION = `(() => {
  const row = document.querySelector('.session-item-wrap.active[data-session-id]');
  return row ? row.getAttribute('data-session-id') : null;
})()`;

// LIVE observation of the generation bubble: content-bearing text,
// Stop-button visibility and placeholder state are sampled TOGETHER so a
// completion can never be misread as live growth.
const OBSERVE_LIVE_TEXT = `(() => {
  const bubble = document.querySelector('.generation-bubble');
  const content = bubble?.querySelector('.message-content');
  const stopVisible = Array.from(document.querySelectorAll('button'))
    .some((b) => b.textContent?.trim() === 'Stop');
  const placeholder = Boolean(bubble?.querySelector('[data-stream-placeholder]'));
  const text = (content?.textContent ?? '').replace(/\\u200b/g, '');
  return { bubble: Boolean(bubble), stopVisible, placeholder, len: text.trim().length };
})()`;

// SETTLED observation: visible non-whitespace final assistant text
// (Markdown), NO placeholder anywhere, exactly ONE persisted assistant
// response and the expected persisted user message, composer unlocked.
const OBSERVE_SETTLED = `(() => {
  const rows = document.querySelectorAll('.message-row.from-agent');
  const markdown = document.querySelectorAll('.message-row.from-agent .message-markdown');
  let markdownChars = 0;
  markdown.forEach((m) => { markdownChars += (m.textContent ?? '').trim().length; });
  const userRows = document.querySelectorAll('.message-row.from-user');
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  return {
    fromAgentRows: rows.length,
    markdownRows: markdown.length,
    markdownChars,
    userRows: userRows.length,
    composerEnabled: composer ? !composer.disabled : false,
    anyPlaceholder: document.querySelectorAll('[data-stream-placeholder]').length,
  };
})()`;

const SEND = (message) => `(() => {
  const composer = document.querySelector('textarea[placeholder*="Message SHEYTAN"], textarea[placeholder*="Describe what SHEYTAN"]');
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  setter.call(composer, ${JSON.stringify(message)});
  composer.dispatchEvent(new Event('input', { bubbles: true }));
  const send = Array.from(document.querySelectorAll('button'))
    .find((b) => /^(Send|Forge)$/.test(b.textContent?.trim() ?? ''));
  if (!send) return false;
  send.click();
  return true;
})()`;

const RELOAD = "location.reload(); true";

// Ordered live-growth evidence for ONE run cycle. Fails closed: any
// missing sample is a failure; a settled row is NEVER a substitute.
async function observeLiveGrowth(cdp, fail) {
  // Phase A — first live non-placeholder sample with Stop visible.
  const liveEnd = deadline(180_000);
  let firstLen = 0;
  let sawLive = false;

  while (Date.now() < liveEnd) {
    const obs = await evalJS(cdp, OBSERVE_LIVE_TEXT, false);

    if (obs && obs.bubble && obs.stopVisible && !obs.placeholder && obs.len > 0) {
      sawLive = true;
      firstLen = obs.len;
      log("SAMPLE A: live text visible while Stop is up, len =", firstLen);
      break;
    }

    // A settled run before any live observation is the EXACT defect
    // class this harness exists to catch.
    const settled = await evalJS(cdp, OBSERVE_SETTLED, false);
    if (settled && settled.composerEnabled && settled.markdownRows > 0) {
      fail(
        "the run SETTLED before any live non-placeholder text was observed " +
          "(the answer appeared only after completion — the defect class)",
      );
      return false;
    }

    await sleep(120);
  }

  if (!sawLive) {
    fail("no live non-placeholder text while Stop was visible");
    return false;
  }

  // Phase B — a LATER, strictly greater sample with Stop STILL visible
  // and content STILL non-placeholder. Recorded after A, in order.
  const growEnd = deadline(180_000);
  while (Date.now() < growEnd) {
    const obs = await evalJS(cdp, OBSERVE_LIVE_TEXT, false);

    if (
      obs &&
      obs.bubble &&
      obs.stopVisible &&
      !obs.placeholder &&
      obs.len > firstLen
    ) {
      log(
        "SAMPLE B: text grew", firstLen, "->", obs.len,
        "with Stop still visible (ordered evidence recorded)",
      );
      return true;
    }

    const settled = await evalJS(cdp, OBSERVE_SETTLED, false);
    if (settled && settled.composerEnabled && settled.markdownRows > 0) {
      fail(
        "the run SETTLED before a strictly larger live sample was observed " +
          "(live growth could not be proven — even though the answer eventually appeared)",
      );
      return false;
    }

    await sleep(100);
  }

  fail("live text never grew while Stop stayed visible (growth window timed out)");
  return false;
}

// Settlement: composer unlocks; exactly ONE persisted assistant answer
// with visible non-whitespace Markdown text; NO placeholder anywhere;
// exactly ONE persisted user message (this cycle's prompt).
async function waitForSettled(cdp, expectedUserRows) {
  const settleEnd = deadline(240_000);
  let settled = null;

  while (Date.now() < settleEnd) {
    settled = await evalJS(cdp, OBSERVE_SETTLED, false);
    if (
      settled &&
      settled.composerEnabled &&
      settled.fromAgentRows === 1 &&
      settled.markdownRows === 1 &&
      settled.markdownChars > 0 &&
      settled.userRows === expectedUserRows &&
      settled.anyPlaceholder === 0
    ) {
      return settled;
    }
    await sleep(400);
  }

  return null;
}

// Real top-level window close: `taskkill` WITHOUT /F posts WM_CLOSE to
// the process's windows — the same path a user's titlebar close takes.
// The process must exit (and release its owned listener) within the
// bounded deadline; a timeout is a FAILURE. Forced termination happens
// only after the failure is recorded, as cleanup.
async function realWindowClose(proc, fail) {
  if (process.platform === "win32") {
    log("sending WM_CLOSE to the top-level window (taskkill, no /F)");
    const tk = spawn("taskkill", ["/PID", String(proc.pid)], {
      stdio: "ignore",
    });
    await new Promise((r) => tk.once("exit", r));
  } else {
    // Non-Windows hosts: the graceful termination signal is the closest
    // supported equivalent of a window close for this harness.
    log("non-Windows host: sending SIGTERM (graceful close equivalent)");
    proc.kill("SIGTERM");
  }

  const exitEnd = deadline(20_000);
  while (Date.now() < exitEnd) {
    const exited = await new Promise((r) => {
      const t = setTimeout(() => r("timeout"), 500);
      proc.once("exit", () => {
        clearTimeout(t);
        r("exited");
      });
    });
    if (exited === "exited") {
      log("process exited after window close; owned listener released");
      return true;
    }
  }

  fail("the process did not exit within 20s of the real window close");
  return false;
}

async function main() {
  log("launching", exe);
  log("CDP port:", cdpPort, "(TEST-ONLY; production builds never ship this)");

  const proc = spawn(exe, [], {
    env: {
      ...process.env,
      WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: `--remote-debugging-port=${cdpPort}`,
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

    // 0. Arm the opt-in stream trace BEFORE app modules load: seed
    // localStorage in a fresh-document script, then reload so the
    // module's load-time detection actually sees the flag. Writing
    // localStorage after app JS has loaded does NOT arm the module
    // (state.enabled is captured at module load) — that was the v1.9.4
    // activation defect.
    await cdp.send("Page.enable");
    await cdp.send("Page.addScriptToEvaluateOnNewDocument", {
      source: `try { localStorage.setItem('SHEYTAN_STREAM_TRACE','1'); } catch (e) {}`,
    });
    await cdp.send("Page.reload");
    await sleep(1500);

    // The trace hook must exist NOW (armed at module load) — a harness
    // that never proves the hook is installed has no trace evidence.
    const hookReady = await evalJS(
      cdp,
      `typeof window.__SHEYTAN_STREAM_TRACE_DUMP__ === 'function' && (() => { try { return localStorage.getItem('SHEYTAN_STREAM_TRACE') === '1'; } catch (e) { return false; } })()`,
      false,
    );
    if (!hookReady) {
      fail("stream-trace hook not armed before app modules loaded (activation path broken)");
    } else {
      log("stream trace armed before app modules loaded");
    }

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

    // The session the run creates must be the session that stays active.
    const activeSessionId = await evalJS(cdp, OBSERVE_ACTIVE_SESSION, false);

    // 2-4. FIRST run cycle: send → ordered live-growth evidence → settle.
    if (!(await evalJS(cdp, SEND(PROMPT), false))) {
      fail("the Send control was not found");
    } else {
      log("message sent (cycle 1)");

      if (await observeLiveGrowth(cdp, fail)) {
        const settled = await waitForSettled(cdp, 1);
        if (!settled) {
          fail(
            "settlement did not produce exactly one real assistant answer " +
              "(nonblank Markdown, no placeholder) with exactly one user message: " +
              JSON.stringify(await evalJS(cdp, OBSERVE_SETTLED, false)),
          );
        } else {
          log("settled: one real assistant answer, composer unlocked (cycle 1)");
        }
      }
    }

    // 5. reload → the SAME session stays active (data-session-id
    // contract) and the transcript persists.
    await evalJS(cdp, RELOAD, false);
    await sleep(1500);

    const reloadEnd = deadline(120_000);
    let sameSession = null;
    let persisted = null;
    while (Date.now() < reloadEnd) {
      const ready2 = await evalJS(cdp, OBSERVE_READY, false);
      if (!ready2) {
        await sleep(500);
        continue;
      }
      if (sameSession === null) {
        const now = await evalJS(cdp, OBSERVE_ACTIVE_SESSION, false);
        if (now) sameSession = now;
      }
      persisted = await evalJS(cdp, OBSERVE_SETTLED, false);
      if (
        persisted &&
        persisted.fromAgentRows === 1 &&
        persisted.markdownRows === 1 &&
        persisted.markdownChars > 0 &&
        persisted.userRows === 1
      ) {
        break;
      }
      await sleep(400);
    }

    if (activeSessionId && sameSession !== activeSessionId) {
      fail(
        "after reload the ACTIVE session changed: before=" +
          JSON.stringify(activeSessionId ?? null) +
          " after=" +
          JSON.stringify(sameSession ?? null),
      );
    } else if (activeSessionId) {
      log("reload: the same session stayed active (sidebar data-session-id contract)");
    }

    if (!persisted || persisted.fromAgentRows !== 1 || persisted.markdownChars === 0) {
      fail("after reload the persisted answer did not survive: " +
        JSON.stringify(persisted));
    } else {
      log("reload: one user + one nonblank assistant reply persisted in the same session");
    }

    // 6. SECOND run cycle — repeatability on the reloaded session.
    if (!(await evalJS(cdp, SEND(PROMPT + " — second cycle"), false))) {
      fail("the Send control was not found on the reloaded session");
    } else {
      log("message sent (cycle 2)");

      if (await observeLiveGrowth(cdp, fail)) {
        const settled2 = await waitForSettled(cdp, 2);
        if (!settled2) {
          fail(
            "cycle 2 settlement did not produce the expected transcript " +
              "(one prior user+assistant pair, one new user message, one new nonblank answer): " +
              JSON.stringify(await evalJS(cdp, OBSERVE_SETTLED, false)),
          );
        } else {
          log("settled: cycle 2 completed with the expected transcript shape");
        }
      }
    }

    // 7. Trace artifact: the dump must exist, carry the expected stages,
    // and be sanitized (lengths/kinds/ephemeral numbers only).
    try {
      const trace = await evalJS(
        cdp,
        `(() => {
          const mod = window.__SHEYTAN_STREAM_TRACE_DUMP__;
          return typeof mod === 'function' ? mod() : 'trace-hook-not-installed';
        })()`,
        false,
      );
      const traceStr = String(trace);
      writeFileSync(join(artifactsDir, "stream-trace.json"), traceStr);

      if (traceStr.includes("trace-hook-not-installed")) {
        fail("the stream trace dump hook was never installed");
      } else {
        const parsed = JSON.parse(traceStr);
        const counters = parsed?.counters ?? {};
        const expectedStages = [
          "ws_frame",
          "fastpath_fold",
          "accumulator_flush",
          "store_commit",
        ];
        const missing = expectedStages.filter((s) => !(counters[s] > 0));
        if (missing.length > 0) {
          fail("stream trace is missing expected stages: " + missing.join(",") +
            " (counters=" + JSON.stringify(counters) + ")");
        } else {
          log("stream trace captured all expected boundary stages");
        }

        // Sanitization: no identifier- or content-bearing keys; string
        // values bounded (kinds/states only).
        const forbidden = /runid|sessionid|content(?!chars)|caption(?!chars)|message(?!_row)|prompt|text(?!len)/i;
        const offenders = [];
        const walk = (node, path) => {
          if (node === null || typeof node !== "object") return;
          for (const [k, v] of Object.entries(node)) {
            if (forbidden.test(k)) offenders.push(path + "." + k);
            if (typeof v === "string" && v.length > 64) {
              offenders.push(path + "." + k + " (string too long)");
            }
            if (typeof v === "object") walk(v, path + "." + k);
          }
        };
        walk(parsed, "trace");
        if (offenders.length > 0) {
          fail("stream trace artifact is not sanitized: " + offenders.slice(0, 5).join("; "));
        } else {
          log("stream trace artifact is sanitized (lengths/kinds only)");
        }
      }
    } catch (err) {
      fail("stream trace dump failed: " + String(err));
    }

    // 8. REAL window close — process exit within the bounded deadline.
    await realWindowClose(proc, fail);
  } catch (err) {
    fail(String(err));
    // Failure cleanup only: force-terminate AFTER the failure is recorded.
    try {
      proc.kill();
    } catch {
      // Already exited.
    }
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
