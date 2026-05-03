import { afterAll, beforeAll, describe, it, expect } from "vitest";
import { cleanup, makeScaffoldTmpdir, pnpmInstallSilent, runPnpm } from "./helpers";

describe("AC-5: pnpm run dev (E2E)", () => {
  let dir: string;

  beforeAll(() => {
    dir = makeScaffoldTmpdir();
    const install = pnpmInstallSilent(dir);
    expect(install.status, `pnpm install failed in tmpdir.\nstderr:\n${install.stderr}`).toBe(0);
  });

  afterAll(() => {
    if (dir) cleanup(dir);
  });

  it('AC-5: pnpm run dev exits without "Missing script" or workspace-config errors', () => {
    const result = runPnpm(["run", "dev"], dir, { timeoutMs: 30_000 });

    expect(result.error, `pnpm spawn error: ${result.error?.message}`).toBeUndefined();
    const startedClean = result.status === 0 || result.status === null;
    expect(startedClean, `pnpm run dev exited ${result.status}.\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`)
      .toBe(true);

    const combined = `${result.stdout}\n${result.stderr}`;
    expect(combined).not.toMatch(/Missing script/i);
    expect(combined).not.toMatch(/ENOENT/);
    expect(combined).not.toMatch(/no projects matched/i);
  });
});
