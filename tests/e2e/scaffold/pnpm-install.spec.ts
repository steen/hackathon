import { existsSync } from "node:fs";
import { join } from "node:path";
import { afterAll, beforeAll, describe, it, expect } from "vitest";
import { cleanup, makeScaffoldTmpdir, runPnpm } from "./helpers";

describe("AC-4: pnpm install on a clean clone (E2E)", () => {
  let dir: string;

  beforeAll(() => {
    dir = makeScaffoldTmpdir();
  });

  afterAll(() => {
    if (dir) cleanup(dir);
  });

  it("AC-4: pnpm install on a clean clone exits 0", () => {
    const result = runPnpm(
      ["install", "--prefer-offline", "--reporter=silent"],
      dir,
      { timeoutMs: 120_000 },
    );

    expect(result.error, `pnpm spawn error: ${result.error?.message}`).toBeUndefined();
    expect(result.status, `pnpm install failed.\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`)
      .toBe(0);

    expect(result.stderr).not.toMatch(/ERR_PNPM_/);

    expect(existsSync(join(dir, "node_modules")), "node_modules not produced").toBe(true);
    expect(existsSync(join(dir, "pnpm-lock.yaml")), "pnpm-lock.yaml not produced").toBe(true);
  });
});
