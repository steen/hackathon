import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";

const repoRoot = resolve(__dirname, "..", "..");
const pkgPath = resolve(repoRoot, "package.json");

interface PackageJSON {
  private?: boolean;
  scripts?: Record<string, string>;
}

describe("AC-3: root package.json declares dev/build/test scripts that fan out", () => {
  it("AC-3: root package.json declares dev/build/test scripts that fan out", () => {
    expect(existsSync(pkgPath)).toBe(true);

    const raw = readFileSync(pkgPath, "utf8");
    const pkg = JSON.parse(raw) as PackageJSON;

    expect(pkg.private).toBe(true);

    for (const script of ["dev", "build", "test"] as const) {
      expect(pkg.scripts, "scripts block missing").toBeTypeOf("object");
      const body = pkg.scripts?.[script] ?? "";
      expect(typeof body, `scripts.${script} must be a string`).toBe("string");
      expect(body.length, `scripts.${script} must be non-empty`).toBeGreaterThan(0);
      expect(
        /pnpm\s+-r\b|pnpm\s+--recursive\b/.test(body),
        `scripts.${script} must fan out across workspaces (pnpm -r ...); got: ${body}`,
      ).toBe(true);
    }
  });
});
