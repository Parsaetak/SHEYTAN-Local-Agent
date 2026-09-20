#!/usr/bin/env node
/**
 * release-version.mjs — Single-source-of-truth release metadata gate for
 * SHEYTAN-Local-Agent.
 *
 * The canonical version lives in package.json ("version", e.g. "1.3.1").
 * Every other release surface is derived from it and must stay in sync:
 *
 *   - internal/config/config.go            ->  AppVersion = "1.3.1"
 *   - build/config.yml                     ->  productVersion: "1.3.1"
 *   - SIGNATURE                            ->  SHEYTAN-Local-Agent v1.3.1
 *
 * The product has a version-only identity: there is no codename dimension,
 * no release suffix and no second version variable. The workflow derives
 * the release identity at runtime from package.json via `--env`, so version
 * metadata has exactly one source and can never drift between files. The
 * workflow is still audited here (shape check) to prove the derivation
 * step has not been dropped.
 *
 * Usage:
 *   node scripts/release-version.mjs            # sync mode (default): repair drift in place
 *   node scripts/release-version.mjs --check    # verify only: exit 1 on any drift
 *   node scripts/release-version.mjs --env      # print APP_VERSION /
 *                                               # APP_VERSION_FULL as VAR=value
 *                                               # lines (CI consumption)
 *
 * Designed for CI: zero dependencies, CRLF-safe, never rewrites a file that
 * is already correct, and emits GitHub Actions ::error:: annotations in
 * check mode so drift is visible directly on the run summary.
 */

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
);

const CHECK_ONLY = process.argv.includes("--check");
const PRINT_ENV = process.argv.includes("--env");

const targets = [
  {
    label: "internal/config/config.go",
    file: "internal/config/config.go",
    // gofmt-aligned const, e.g. `      AppVersion = "1.3.1"` (whitespace-
    // flexible: the alignment depends on the identifiers in the block)
    pattern: /((?:^|\r?\n)[ \t]*AppVersion[ \t]*=[ \t]*")([^"]*)(")/,
    expected: (version) => version,
    describe: (v) => `AppVersion = "${v}"`,
  },
  {
    label: "build/config.yml",
    file: join("build", "config.yml"),
    pattern: /((?:^|\r?\n)[ \t]*productVersion:[ \t]*")([^"]*)(")/,
    expected: (version) => version,
    describe: (v) => `productVersion: "${v}"`,
  },
  {
    label: "SIGNATURE",
    file: "SIGNATURE",
    // First line only: "SHEYTAN-Local-Agent v1.3.1"
    pattern: /(^[ \t]*SHEYTAN-Local-Agent[ \t]+v)([^\r\n]*)/,
    multiline: true,
    // group(2) holds only the version token after the "SHEYTAN-Local-Agent v"
    // prefix, so the splice value is the bare canonical version.
    expected: (version) => version,
    describe: (v) => `first line = "SHEYTAN-Local-Agent v${v}"`,
  },
  {
    // Shape check: the workflow must derive APP_VERSION at runtime
    // (see --env) instead of pinning a stale hardcoded version constant.
    label: ".github/workflows/build-desktop.yml",
    file: join(".github", "workflows", "build-desktop.yml"),
    // Two capture groups (prefix + marker) keep the generic splice path
    // happy: group(2) is compared against expected() and never differs,
    // so this target only fails when the marker is absent entirely.
    pattern: /((?:release-version\.mjs[ \t]+))(--env)/,
    expected: () => "--env",
    describe: () => "runtime identity derivation (release-version.mjs --env)",
  },
];

function fail(message) {
  if (CHECK_ONLY) {
    // GitHub Actions annotation so the drift shows up on the run summary.
    console.error(`::error::${message}`);
  }
  console.error(`[sheytan-release] ERROR: ${message}`);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// 1. Read the canonical version from package.json.
// ---------------------------------------------------------------------------

let packageVersion;
try {
  const pkg = JSON.parse(readFileSync(join(repoRoot, "package.json"), "utf8"));
  packageVersion = pkg.version;
} catch (error) {
  fail(`unable to read package.json: ${error.message}`);
}

if (typeof packageVersion !== "string" || packageVersion.length === 0) {
  fail("package.json has no usable \"version\" field.");
}

const semverMatch = packageVersion.match(/^(\d+)\.(\d+)\.(\d+)$/);

if (!semverMatch) {
  fail(
    `package.json version "${packageVersion}" is not plain semver ` +
      `(expected exactly "MAJOR.MINOR.PATCH", e.g. "1.3.1").`,
  );
}

const version = packageVersion;

console.log(
  `[sheytan-release] source of truth: package.json = ${version}`,
);

// ---------------------------------------------------------------------------
// 1b. --env mode: emit the release identity for CI consumption.
// ---------------------------------------------------------------------------

if (PRINT_ENV) {
  // One VAR=value per line so both bash ($GITHUB_ENV) and PowerShell
  // ($env:) consumers can parse it with a naive split on the first "=".
  // APP_VERSION       canonical semver  e.g. "1.3.1"
  // APP_VERSION_FULL  compatibility alias — equals APP_VERSION exactly;
  //                   retained so existing consumers keep working while
  //                   the identity stays one-dimensional.
  console.log(`APP_VERSION=${version}`);
  console.log(`APP_VERSION_FULL=${version}`);
  process.exit(0);
}

// ---------------------------------------------------------------------------
// 2. Verify (and, in sync mode, repair) every derived release surface.
// ---------------------------------------------------------------------------

let driftFound = 0;
let repaired = 0;

for (const target of targets) {
  const path = join(repoRoot, target.file);

  let content;
  try {
    content = readFileSync(path, "utf8");
  } catch (error) {
    fail(`unable to read ${target.label}: ${error.message}`);
  }

  const pattern = target.multiline
    ? new RegExp(target.pattern.source, "m")
    : target.pattern;

  const match = content.match(pattern);

  if (!match) {
    fail(
      `${target.label}: release metadata marker not found ` +
        `(expected something matching ${target.describe("<version>")}).`,
    );
  }

  const current = match[2];
  const wanted = target.expected(version);

  if (current === wanted) {
    console.log(
      `[sheytan-release] ${target.label}: OK (${target.describe(current)})`,
    );
    continue;
  }

  driftFound += 1;

  if (CHECK_ONLY) {
    console.error(
      `[sheytan-release] DRIFT in ${target.label}: expected ` +
        `${target.describe(wanted)}, found ${target.describe(current)}`,
    );
    continue;
  }

  // Sync mode: splice the new value into the matched span, leaving every
  // other byte (indentation, alignment, line endings) untouched.
  const start = match.index + match[1].length;
  const end = start + match[2].length;
  const updated = content.slice(0, start) + wanted + content.slice(end);

  writeFileSync(path, updated, "utf8");
  repaired += 1;

  console.log(
    `[sheytan-release] ${target.label}: repaired ` +
      `${target.describe(current)} -> ${target.describe(wanted)}`,
  );
}

// ---------------------------------------------------------------------------
// 3. Report.
// ---------------------------------------------------------------------------

if (CHECK_ONLY) {
  if (driftFound > 0) {
    console.error(
      `[sheytan-release] ${driftFound} release metadata drift(s) detected. ` +
        `Run "node scripts/release-version.mjs" (sync mode) to repair, ` +
        `then commit the result.`,
    );
    process.exit(1);
  }
  console.log("[sheytan-release] release metadata consistent (check mode).");
  process.exit(0);
}

if (repaired === 0) {
  console.log("[sheytan-release] release metadata consistent (sync mode).");
} else {
  console.log(
    `[sheytan-release] ${repaired} file(s) repaired from package.json ` +
      `version ${version}.`,
  );
}
