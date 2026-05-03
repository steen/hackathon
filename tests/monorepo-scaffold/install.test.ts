import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";
import { parse as parseYaml } from "yaml";

const repoRoot = resolve(__dirname, "..", "..");

describe("monorepo-scaffold AC-4: pnpm install from a clean clone succeeds", () => {
  it("AC-4 monorepo-scaffold: pnpm-lock.yaml exists at repo root", () => {
    expect(existsSync(resolve(repoRoot, "pnpm-lock.yaml"))).toBe(true);
  });

  it("AC-4 monorepo-scaffold: pnpm-lock.yaml parses as valid YAML and references each declared workspace package", () => {
    const lockPath = resolve(repoRoot, "pnpm-lock.yaml");
    const lockRaw = readFileSync(lockPath, "utf8");
    const lock = parseYaml(lockRaw) as { importers?: Record<string, unknown> };
    expect(lock).toBeTypeOf("object");
    expect(lock).not.toBeNull();

    const workspaceRaw = readFileSync(resolve(repoRoot, "pnpm-workspace.yaml"), "utf8");
    const workspace = parseYaml(workspaceRaw) as { packages: string[] };
    expect(Array.isArray(workspace.packages)).toBe(true);

    const importerKeys = Object.keys(lock.importers ?? {});
    expect(importerKeys.length, "lockfile must declare at least the root importer").toBeGreaterThan(
      0,
    );
    expect(importerKeys, "root importer key '.' missing from lockfile").toContain(".");
  });
});
