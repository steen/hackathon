import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";
import { parse as parseYaml } from "yaml";

const repoRoot = resolve(__dirname, "..", "..");
const workspacePath = resolve(repoRoot, "pnpm-workspace.yaml");

describe("AC-2: pnpm-workspace.yaml declares apps/* and packages/*", () => {
  it("AC-2: pnpm-workspace.yaml exists at repo root", () => {
    expect(existsSync(workspacePath)).toBe(true);
  });

  it("AC-2: pnpm-workspace.yaml parses cleanly and packages includes apps/* and packages/*", () => {
    const raw = readFileSync(workspacePath, "utf8");
    const parsed = parseYaml(raw) as { packages?: string[] };

    expect(parsed).toBeTypeOf("object");
    expect(parsed).not.toBeNull();
    expect(Array.isArray(parsed.packages)).toBe(true);
    expect(parsed.packages).toContain("apps/*");
    expect(parsed.packages).toContain("packages/*");
  });
});
