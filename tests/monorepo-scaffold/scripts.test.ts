import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";

const repoRoot = resolve(__dirname, "..", "..");
const pkg = JSON.parse(readFileSync(resolve(repoRoot, "package.json"), "utf8"));

describe("monorepo-scaffold AC-5: top-level scripts run without configuration errors", () => {
  for (const script of ["dev", "build", "test"] as const) {
    it(`AC-5 monorepo-scaffold: scripts.${script} fans out across workspaces and uses --if-present so empty workspaces do not cause configuration errors`, () => {
      const body = pkg.scripts?.[script];
      expect(typeof body, `scripts.${script} must be a string`).toBe("string");
      expect(/pnpm\s+(?:-r|--recursive)\b/.test(body), `scripts.${script} must use pnpm -r; got: ${body}`).toBe(true);
      expect(/--if-present\b/.test(body), `scripts.${script} must include --if-present so empty workspaces don't fail with "missing script"; got: ${body}`).toBe(true);
    });
  }
});
