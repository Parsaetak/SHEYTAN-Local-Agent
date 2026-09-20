#!/usr/bin/env node
/**
 * verify-static-assets.mjs — embedded frontend acceptance contract.
 *
 * The embedded frontend (web/static) must satisfy a STABLE FILENAME
 * contract:
 *
 *   1. web/static/index.html exists and references /assets/index.js and
 *      /assets/index.css.
 *   2. Every asset referenced by index.html (script src, stylesheet href,
 *      modulepreload) physically exists under web/static.
 *   3. Every dynamic-import chunk referenced by the bundled JS exists
 *      (lazy panels must never 404 at runtime).
 *   4. NO generated asset carries a content hash — filenames such as
 *      index-DRb7l71I.js or AgentBody-jPzETmLT.js are rejected. The same
 *      physical file must keep the same name across builds so the agent
 *      can always identify it.
 *   5. When a fresh dist/ build exists (CI: clean build → sync-web), the
 *      file sets of dist/ and web/static/ must be IDENTICAL — no stale
 *      files from a previous generation may survive the sync.
 *   6. No .vite/ manifest is generated (no runtime consumer exists).
 *
 * Usage:
 *   node scripts/verify-static-assets.mjs            # verify web/static
 *   node scripts/verify-static-assets.mjs --dist     # also diff vs dist/
 *
 * Exit code 1 on any violation. Designed for CI: zero dependencies.
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const staticDir = join(repoRoot, "web", "static");
const distDir = join(repoRoot, "dist");

const COMPARE_DIST = process.argv.includes("--dist");

const errors = [];

function fail(message) {
  errors.push(message);
}

// ---------------------------------------------------------------------------
// 1. index.html exists and carries the stable entry references.
// ---------------------------------------------------------------------------

const indexPath = join(staticDir, "index.html");

if (!existsSync(indexPath)) {
  console.error(`::error::web/static/index.html is missing`);
  process.exit(1);
}

const indexHtml = readFileSync(indexPath, "utf8");

if (!indexHtml.includes('<div id="root"></div>')) {
  fail("index.html does not contain the root mount element");
}

for (const required of ["/assets/index.js", "/assets/index.css"]) {
  if (!indexHtml.includes(`"${required}"`)) {
    fail(`index.html does not reference the stable entry ${required}`);
  }
}

// ---------------------------------------------------------------------------
// 2. Every referenced asset exists.
// ---------------------------------------------------------------------------

const referenceRe =
  /(?:src|href)=["']([^"']+\.(?:js|css))["']/g;

for (const match of indexHtml.matchAll(referenceRe)) {
  const ref = match[1];
  const assetPath = join(staticDir, ref.replace(/^\//, ""));
  if (!existsSync(assetPath)) {
    fail(`index.html references ${ref} but the file does not exist`);
  }
}

// ---------------------------------------------------------------------------
// 3. Dynamic-import chunks referenced by the bundle exist.
// ---------------------------------------------------------------------------

function walk(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full, out);
    } else if (entry.name.endsWith(".js")) {
      out.push(full);
    }
  }
  return out;
}

const jsFiles = existsSync(join(staticDir, "assets"))
  ? walk(join(staticDir, "assets"))
  : [];

// Vite/Rolldown emit dynamic imports as import("./Chunk.js") (or with
// backtick template literals) — relative specifiers inside the emitting
// file. Grab them all and resolve. If a bundle contains NO dynamic
// import at all that is itself a contract violation for the split app.
const dynamicImportRe = /import\(\s*(?:["'`])(\.{1,2}\/[^"'`]+\.js)(?:["'`])\s*\)/g;

let dynamicImportsFound = 0;

for (const jsFile of jsFiles) {
  const content = readFileSync(jsFile, "utf8");
  for (const match of content.matchAll(dynamicImportRe)) {
    dynamicImportsFound += 1;
    const spec = match[1];
    const target = resolve(dirname(jsFile), spec);
    if (!existsSync(target)) {
      fail(
        `${relative(repoRoot, jsFile)} dynamically imports ${spec} but the chunk does not exist`,
      );
    }
  }
}

// The application is code-split (lazy panels). A bundle with ZERO
// dynamic imports means splitting was silently disabled.
if (dynamicImportsFound === 0) {
  fail(
    "no dynamic imports found in the bundled JS — code splitting appears disabled",
  );
}

// ---------------------------------------------------------------------------
// 4. No hashed filenames in the generated tree.
// ---------------------------------------------------------------------------

// A content hash suffix looks like index-DRb7l71I.js or index-CSN2ThP8.css:
// a hyphen followed by a base62-ish segment directly before the extension.
// Real Vite/Rolldown hashes always contain at least one digit; stable
// suffixes that are plain words (rolldown-runtime.js) do not. Stable names
// are otherwise bare (index.js, AgentBody.js, index.css).
const hashSuffixRe = /-([A-Za-z0-9_]{6,})\.(?:js|mjs|css|woff2?|ttf|otf|png|jpg|jpeg|svg|webp)$/;

for (const file of walk(staticDir)) {
  const base = relative(staticDir, file);
  const match = base.match(hashSuffixRe);
  if (match && match[1].split("").some((ch) => ch >= "0" && ch <= "9")) {
    fail(`hashed asset filename is forbidden (stable names required): ${base}`);
  }
}

if (existsSync(join(staticDir, ".vite", "manifest.json"))) {
  fail("web/static/.vite/manifest.json exists but no runtime consumer does");
}

// ---------------------------------------------------------------------------
// 5. Optional: web/static must exactly mirror the current dist/ build.
// ---------------------------------------------------------------------------

function collectFiles(dir, prefix = "") {
  const out = new Set();
  if (!existsSync(dir)) {
    return out;
  }
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const rel = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      for (const f of collectFiles(join(dir, entry.name), rel)) {
        out.add(f);
      }
    } else if (statSync(join(dir, entry.name)).isFile()) {
      out.add(rel);
    }
  }
  return out;
}

if (COMPARE_DIST) {
  const distFiles = collectFiles(distDir);
  const staticFiles = collectFiles(staticDir);

  if (distFiles.size === 0) {
    fail("--dist given but dist/ is empty — run the frontend build first");
  }

  const stale = [...staticFiles].filter((f) => !distFiles.has(f));
  const missing = [...distFiles].filter((f) => !staticFiles.has(f));

  for (const f of stale) {
    fail(`web/static contains a file the current build does not produce: ${f}`);
  }
  for (const f of missing) {
    fail(`web/static is missing a file the current build produces: ${f}`);
  }
}

// ---------------------------------------------------------------------------
// Report.
// ---------------------------------------------------------------------------

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`::error::${error}`);
    console.error(`[verify-static-assets] ${error}`);
  }
  console.error(
    `[verify-static-assets] ${errors.length} violation(s) — the embedded frontend does not satisfy the stable-asset contract.`,
  );
  process.exit(1);
}

console.log(
  "[verify-static-assets] stable-asset contract satisfied" +
    (COMPARE_DIST ? " (web/static mirrors the current build)" : "") +
    ".",
);
