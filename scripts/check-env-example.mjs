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
export const ENV_CONST_PATTERN = /\bEnv[A-Z]\w*\s*=\s*"([^"]+)"/g;

// Match `<lowercase>Env = "VALUE"` — the legacy const block in main.go uses
// `portEnv`, `dbPathEnv`, etc. We intentionally exclude the canonical
// `Env*` form here so a single const isn't reported twice.
//
// This pattern is run against the body of the first package-level
// `const ( ... )` block only (see extractFirstConstBlock below), and against
// a comment-stripped copy of that body (see stripGoComments). Without the
// const-block anchor, any `<lowercase>Env = "CHAT_..."` assignment elsewhere
// in the file — a function-local sentinel, a var block, a struct tag —
// would be picked up. Without comment-stripping, a commented-out binding
// like `// fooEnv = "CHAT_..."` would be a false positive.
export const LEGACY_ENV_CONST_PATTERN = /\b[a-z]\w*Env\s*=\s*"([^"]+)"/g;

// extractFirstConstBlock returns the body of the first package-level
// `const ( ... )` block in `source`, or empty string when no such block
// exists. The body excludes the `const (` opener and the trailing `)`.
//
// The walker is string- and comment-aware: a `(` or `)` inside an
// interpreted string (`"..."`), raw string (\`...\`), rune literal
// (`'...'`), line comment (`// ...`), or block comment (`/* ... */`)
// does NOT change the paren-depth counter. Without this, a const like
//   foo = "(unbalanced"
// would close the block prematurely (or never), and the legacy regex
// would be run against the wrong slice.
export function extractFirstConstBlock(source) {
  const opener = findConstOpener(source);
  if (opener === -1) return "";
  let depth = 1;
  let i = opener;
  const start = i;
  while (i < source.length) {
    const ch = source[i];
    const next = source[i + 1];

    if (ch === "/" && next === "/") {
      // Line comment runs to the next newline (or EOF).
      const nl = source.indexOf("\n", i + 2);
      i = nl === -1 ? source.length : nl + 1;
      continue;
    }
    if (ch === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2);
      i = end === -1 ? source.length : end + 2;
      continue;
    }
    if (ch === '"') {
      i = skipInterpretedString(source, i + 1);
      continue;
    }
    if (ch === "`") {
      const end = source.indexOf("`", i + 1);
      i = end === -1 ? source.length : end + 1;
      continue;
    }
    if (ch === "'") {
      i = skipRuneLiteral(source, i + 1);
      continue;
    }
    if (ch === "(") {
      depth += 1;
    } else if (ch === ")") {
      depth -= 1;
      if (depth === 0) return source.slice(start, i);
    }
    i += 1;
  }
  return "";
}

// findConstOpener returns the index of the byte immediately after the
// opening `(` of the first package-level `const ( ... )` block, or -1
// when no such block exists. The `const` keyword itself can appear inside
// strings or comments; this scan honors those lexical contexts so a
// commented-out `// const (` doesn't shadow the real opener.
function findConstOpener(source) {
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    const next = source[i + 1];

    if (ch === "/" && next === "/") {
      const nl = source.indexOf("\n", i + 2);
      i = nl === -1 ? source.length : nl + 1;
      continue;
    }
    if (ch === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2);
      i = end === -1 ? source.length : end + 2;
      continue;
    }
    if (ch === '"') {
      i = skipInterpretedString(source, i + 1);
      continue;
    }
    if (ch === "`") {
      const end = source.indexOf("`", i + 1);
      i = end === -1 ? source.length : end + 1;
      continue;
    }
    if (ch === "'") {
      i = skipRuneLiteral(source, i + 1);
      continue;
    }
    if (
      ch === "c" &&
      source.startsWith("const", i) &&
      isIdentBoundaryBefore(source, i) &&
      !isIdentChar(source[i + 5])
    ) {
      // Found `const` at a token boundary; advance past whitespace /
      // comments to look for `(`. If anything else appears first
      // (e.g. `const Foo = 1` — single-binding form) keep scanning.
      let j = i + 5;
      while (j < source.length) {
        const cj = source[j];
        const cn = source[j + 1];
        if (cj === " " || cj === "\t" || cj === "\n" || cj === "\r") {
          j += 1;
          continue;
        }
        if (cj === "/" && cn === "/") {
          const nl = source.indexOf("\n", j + 2);
          j = nl === -1 ? source.length : nl + 1;
          continue;
        }
        if (cj === "/" && cn === "*") {
          const end = source.indexOf("*/", j + 2);
          j = end === -1 ? source.length : end + 2;
          continue;
        }
        if (cj === "(") return j + 1;
        // Anything else means this is a single-const, not a block — keep
        // scanning for the next `const`.
        break;
      }
      i += 5;
      continue;
    }
    i += 1;
  }
  return -1;
}

function isIdentChar(ch) {
  return ch !== undefined && /[A-Za-z0-9_]/.test(ch);
}

function isIdentBoundaryBefore(source, i) {
  if (i === 0) return true;
  return !isIdentChar(source[i - 1]);
}

// skipInterpretedString returns the index immediately after the closing
// `"` of an interpreted Go string starting at `start` (i.e. caller has
// already consumed the opening `"`). Honors `\\` and `\"` escapes; an
// unterminated literal returns source.length.
function skipInterpretedString(source, start) {
  let i = start;
  while (i < source.length) {
    const ch = source[i];
    if (ch === "\\") {
      i += 2;
      continue;
    }
    if (ch === '"') return i + 1;
    if (ch === "\n") return i + 1; // Go interpreted strings can't span newlines; bail.
    i += 1;
  }
  return source.length;
}

// skipRuneLiteral returns the index immediately after the closing `'` of
// a Go rune literal starting at `start`. Honors `\\` escapes; an
// unterminated literal returns source.length.
function skipRuneLiteral(source, start) {
  let i = start;
  while (i < source.length) {
    const ch = source[i];
    if (ch === "\\") {
      i += 2;
      continue;
    }
    if (ch === "'") return i + 1;
    if (ch === "\n") return i + 1;
    i += 1;
  }
  return source.length;
}

// stripGoComments removes line and block comments from a Go source slice,
// preserving the surrounding code byte-for-byte (comments collapse to a
// single space so adjacent tokens stay separated). String- and
// rune-literal contents are passed through unchanged so a `//` inside a
// string isn't treated as a comment.
export function stripGoComments(source) {
  let out = "";
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    const next = source[i + 1];

    if (ch === "/" && next === "/") {
      const nl = source.indexOf("\n", i + 2);
      out += " ";
      i = nl === -1 ? source.length : nl;
      continue;
    }
    if (ch === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2);
      out += " ";
      i = end === -1 ? source.length : end + 2;
      continue;
    }
    if (ch === '"') {
      const end = skipInterpretedString(source, i + 1);
      out += source.slice(i, end);
      i = end;
      continue;
    }
    if (ch === "`") {
      const close = source.indexOf("`", i + 1);
      const end = close === -1 ? source.length : close + 1;
      out += source.slice(i, end);
      i = end;
      continue;
    }
    if (ch === "'") {
      const end = skipRuneLiteral(source, i + 1);
      out += source.slice(i, end);
      i = end;
      continue;
    }
    out += ch;
    i += 1;
  }
  return out;
}

function readSource(path) {
  try {
    return readFileSync(path, "utf8");
  } catch (err) {
    console.error(`could not read ${path}: ${err.message}`);
    process.exit(2);
  }
}

export function collectMatches(source, pattern, sourcePath) {
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
  const mainConstBlockNoComments = stripGoComments(mainConstBlock);
  for (const { name, source } of collectMatches(
    mainConstBlockNoComments,
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

// Only run when invoked as a script — importing for tests must not exit.
if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
