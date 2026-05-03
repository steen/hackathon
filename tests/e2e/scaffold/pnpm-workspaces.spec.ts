import { describe, it, expect } from "vitest";
import { repoRoot, runPnpm } from "./helpers";

describe("AC-2: pnpm workspace discovery (E2E)", () => {
  it("AC-2: pnpm sees declared workspaces from a clean clone", () => {
    const result = runPnpm(["m", "ls", "--json", "--depth=-1"], repoRoot, {
      timeoutMs: 60_000,
    });

    expect(result.error, `pnpm spawn error: ${result.error?.message}`).toBeUndefined();
    expect(result.status, `stderr:\n${result.stderr}`).toBe(0);

    const parsed: Array<{ name?: string; path?: string }> = JSON.parse(result.stdout);
    const names = parsed.map((p) => p.name).filter(Boolean) as string[];
    const paths = parsed.map((p) => p.path).filter(Boolean) as string[];

    expect(names, `pnpm m ls returned no workspace packages: ${result.stdout}`)
      .toContain("@hackathon/scaffold-stub");

    const hasAppsOrPackagesEntry = paths.some(
      (p) => p.includes("/packages/") || p.includes("/apps/"),
    );
    expect(hasAppsOrPackagesEntry, `expected an apps/* or packages/* entry; got: ${paths.join(", ")}`)
      .toBe(true);
  });
});
