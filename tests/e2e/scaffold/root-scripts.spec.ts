import { afterAll, beforeAll, describe, it, expect } from "vitest";
import { cleanup, makeScaffoldTmpdir, pnpmInstallSilent, runPnpm } from "./helpers";

describe("AC-3: root package.json scripts dispatch via pnpm -r (E2E)", () => {
  let dir: string;

  beforeAll(() => {
    dir = makeScaffoldTmpdir();
    const install = pnpmInstallSilent(dir);
    expect(install.status, `pnpm install failed in tmpdir.\nstderr:\n${install.stderr}`).toBe(0);
  });

  afterAll(() => {
    if (dir) cleanup(dir);
  });

  it("AC-3: root scripts dispatch to workspaces via pnpm -r", () => {
    const result = runPnpm(["run", "build"], dir, { timeoutMs: 60_000 });

    expect(result.error, `pnpm spawn error: ${result.error?.message}`).toBeUndefined();
    expect(result.status, `pnpm run build failed.\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`)
      .toBe(0);

    const combined = `${result.stdout}\n${result.stderr}`;
    expect(combined).not.toMatch(/Missing script/i);

    const dispatched =
      /scaffold-stub/.test(combined) ||
      /@hackathon\/scaffold-stub/.test(combined) ||
      /pnpm\s+-r/.test(combined);
    expect(dispatched, `expected output to reference workspace dispatch.\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`)
      .toBe(true);
  });
});
