#!/usr/bin/env node
// Asserts that every server env-var constant declared in the Go source is
// mentioned in `.env.example`. Mirrors the pattern of
// `scripts/check-workspace-exports.mjs`: hand-runnable, regex-based, no
// build step; non-zero exit on drift names the offenders.
//
// Sources scanned:
//   - apps/server/internal/config/config.go — every `Env[A-Z]\w* = "..."`
//     const (the canonical names; see lines 17-25 at time of writing).
//   - apps/server/main.go                    — every `<lowercase>Env = "..."`
//     const inside the package-level `const ( ... )` block (legacy /
//     transitional names like `portEnv`, `dbPathEnv`, `allowedOriginsEnv`,
//     currently lines 22-26). The legacy pattern is run against the const
//     block body only, not the whole file, so an unrelated identifier
//     ending in `Env` declared in a `var` block or function body cannot
//     leak into the required-set.
//
// If a constant's string value is missing from `.env.example`, exit 1 and
// list each offender with its source location and expected env var name.

import console from "node:console";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");

const CONFIG_GO = join(repoRoot, "apps", "server", "internal", "config", "config.go");
const MAIN_GO = join(repoRoot, "apps", "server", "main.go");
const ENV_EXAMPLE = join(repoRoot, ".env.example");

// Match `Env<Name> = "VALUE"` — covers both `EnvJWTSecret = "CHAT_JWT_SECRET"`
// in config.go and any future Env-prefixed const that follows the same
// shape. The `\b` boundary ensures we don't pick up variables like
// `someEnvSetting`.
const ENV_CONST_PATTERN = /\bEnv[A-Z]\w*\s*=\s*"([^"]+)"/g;

// Match `<lowercase>Env = "VALUE"` — the legacy const block in main.go uses
// `portEnv`, `dbPathEnv`, etc. We intentionally exclude the canonical
// `Env*` form here so a single const isn't reported twice.
//
// This pattern is run against the body of the first package-level
// `const ( ... )` block only (see extractFirstConstBlock below). Without
// that anchor, any `<lowercase>Env = "CHAT_..."` assignment elsewhere in
// the file — a function-local sentinel, a var block, a struct tag — would
// be picked up. The const block is the contract; the scan honors it.
const LEGACY_ENV_CONST_PATTERN = /\b[a-z]\w*Env\s*=\s*"([^"]+)"/g;

// extractFirstConstBlock returns the body of the first package-level
// `const ( ... )` block in `source`, or empty string when no such block
// exists. The body excludes the `const (` opener and the trailing `)`.
//
// Go forbids nesting `const ( ... )` blocks, so a simple paren-depth
// counter from the opening `(` is sufficient — no need to track string
// literals or comments specially: the only `)` that matters is the one
// at depth 0, and the legacy regex looks for `name = "..."` shapes which
// don't appear inside a single `"..."` string in normal Go source.
function extractFirstConstBlock(source) {
  const opener = /\bconst\s*\(/.exec(source);
  if (!opener) return "";
  let depth = 1;
  let i = opener.index + opener[0].length;
  const start = i;
  for (; i < source.length; i++) {
    const ch = source[i];
    if (ch === "(") depth += 1;
    else if (ch === ")") {
      depth -= 1;
      if (depth === 0) return source.slice(start, i);
    }
  }
  return "";
}

function readSource(path) {
  try {
    return readFileSync(path, "utf8");
  } catch (err) {
    console.error(`could not read ${path}: ${err.message}`);
    process.exit(2);
  }
}

function collectMatches(source, pattern, sourcePath) {
  const found = [];
  for (const match of source.matchAll(pattern)) {
    const value = match[1];
    if (!value.startsWith("CHAT_")) {
      // The Go source declares a few non-env sentinels (e.g. defaults like
      // `DefaultListenAddr`); skip anything that isn't shaped like a
      // CHAT_* env var name so the check stays tight.
      continue;
    }
    found.push({ name: value, source: sourcePath });
  }
  return found;
}

function main() {
  const configSrc = readSource(CONFIG_GO);
  const mainSrc = readSource(MAIN_GO);
  const envExample = readSource(ENV_EXAMPLE);

  const required = new Map();
  for (const { name, source } of collectMatches(configSrc, ENV_CONST_PATTERN, CONFIG_GO)) {
    if (!required.has(name)) required.set(name, source);
  }
  const mainConstBlock = extractFirstConstBlock(mainSrc);
  if (mainConstBlock === "") {
    console.error(
      `check-env-example: could not locate a package-level const block in ${MAIN_GO}; ` +
        "the legacy regex requires one — refusing to scan the whole file.",
    );
    process.exit(2);
  }
  for (const { name, source } of collectMatches(
    mainConstBlock,
    LEGACY_ENV_CONST_PATTERN,
    MAIN_GO,
  )) {
    if (!required.has(name)) required.set(name, source);
  }

  if (required.size === 0) {
    console.error(
      "check-env-example: no env-var constants found in Go source — " +
        "regex drift? expected matches in apps/server/internal/config/config.go " +
        "and apps/server/main.go.",
    );
    process.exit(2);
  }

  const missing = [];
  for (const [name, source] of required) {
    // Anchor the search so e.g. `CHAT_LISTEN_ADDR` doesn't accidentally
    // match `CHAT_LISTEN_ADDR_OLD`. Word-boundary plus the `=` that every
    // `KEY=value` line has.
    const re = new RegExp(`(^|\\n)\\s*#?\\s*${name}=`);
    if (!re.test(envExample)) {
      missing.push({ name, source });
    }
  }

  if (missing.length > 0) {
    console.error("check-env-example: .env.example is missing the following env vars:");
    for (const { name, source } of missing) {
      console.error(`  - ${name}  (declared in ${source})`);
    }
    console.error(
      "\nFix: add a `${NAME}=…` line (commented or with a placeholder " +
        "value) to .env.example and document it in README.md's " +
        "'Server environment variables' table.",
    );
    process.exit(1);
  }

  console.log(`check-env-example: ok (${required.size} env vars covered)`);
}

main();
