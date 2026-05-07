#!/usr/bin/env node
// Asserts that every in-repo TS workspace package's `main`/`types`/`exports`
// resolves from source (./src/...), not from `./dist/`. Lint and typecheck
// run before any build in CI; if entry points point at dist, consumers see
// `any` and either fail the type-aware lint pass or pass it falsely.
//
// Walks `packages/*/package.json` and exits non-zero when any TS package
// (i.e. one that declares `main`/`types`/`exports`) uses a `./dist/...`
// path. Non-TS packages (no entry-point fields) are ignored — go-client
// is Go.

import console from "node:console";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const packagesDir = join(repoRoot, "packages");

const ENTRY_FIELDS = ["main", "types", "module"];

function collectExportPaths(value, out) {
  if (typeof value === "string") {
    out.push(value);
    return;
  }
  if (value && typeof value === "object") {
    for (const v of Object.values(value)) {
      collectExportPaths(v, out);
    }
  }
}

function isDistPath(p) {
  return /(^|\/)dist(\/|$)/.test(p);
}

function isSourcePath(p) {
  return /\.(tsx?|css)$/.test(p);
}

function checkPackage(pkgDir) {
  const pkgJsonPath = join(pkgDir, "package.json");
  let raw;
  try {
    raw = readFileSync(pkgJsonPath, "utf8");
  } catch {
    return [];
  }
  const pkg = JSON.parse(raw);

  const paths = [];
  for (const field of ENTRY_FIELDS) {
    if (typeof pkg[field] === "string") {
      paths.push({ field, path: pkg[field] });
    }
  }
  if (pkg.exports !== undefined) {
    const exportPaths = [];
    collectExportPaths(pkg.exports, exportPaths);
    for (const p of exportPaths) {
      paths.push({ field: "exports", path: p });
    }
  }

  if (paths.length === 0) {
    return [];
  }

  const violations = [];
  for (const { field, path: p } of paths) {
    if (isDistPath(p)) {
      violations.push(
        `${pkg.name ?? pkgDir}: ${field} points at "${p}" — must resolve from source (./src/*.ts) so lint/typecheck work without a prior build.`,
      );
      continue;
    }
    if (!isSourcePath(p)) {
      violations.push(
        `${pkg.name ?? pkgDir}: ${field} points at "${p}" — expected a .ts/.tsx source file.`,
      );
    }
  }
  return violations;
}

function main() {
  let entries;
  try {
    entries = readdirSync(packagesDir);
  } catch {
    console.error(`packages directory not found at ${packagesDir}`);
    process.exit(2);
  }

  const allViolations = [];
  for (const name of entries) {
    const pkgDir = join(packagesDir, name);
    let s;
    try {
      s = statSync(pkgDir);
    } catch {
      continue;
    }
    if (!s.isDirectory()) continue;
    allViolations.push(...checkPackage(pkgDir));
  }

  if (allViolations.length > 0) {
    console.error("workspace-exports check failed:");
    for (const v of allViolations) {
      console.error(`  - ${v}`);
    }
    console.error(
      "\nFix: repoint main/types/exports at ./src/index.ts (preferred) " +
        "or add a prebuild hook before lint runs in CI.",
    );
    process.exit(1);
  }

  console.log("workspace-exports: ok");
}

main();
