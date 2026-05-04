// AC-7: Builds via `tsc` and is consumable by `apps/web` through
// pnpm workspace resolution.
//
// Drives `pnpm --filter @hackathon/api-client typecheck` (which
// invokes `tsc --noEmit` against tsconfig.build.json) and
// `pnpm --filter web typecheck` (which builds the web app's TS,
// resolving the workspace import) — exit 0 means tsc accepted the
// package surface, including from the consumer's perspective.

import { describe, it, expect } from "vitest";
import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..", "..");

function runPnpm(args: string[]): { status: number | null; stdout: string; stderr: string } {
  const r = spawnSync("pnpm", args, {
    cwd: repoRoot,
    encoding: "utf8",
    env: { ...process.env, CI: "1" },
  });
  return { status: r.status, stdout: r.stdout, stderr: r.stderr };
}

describe("AC-7: api-client builds via tsc and is consumable by apps/web through workspace resolution", () => {
  it("AC-7: `pnpm --filter @hackathon/api-client typecheck` exits 0", () => {
    const r = runPnpm(["--filter", "@hackathon/api-client", "typecheck"]);
    if (r.status !== 0) {
      // Surface the tsc output in the failure for triage.
      throw new Error(
        `api-client typecheck failed (status=${String(r.status)})\nSTDOUT:\n${r.stdout}\nSTDERR:\n${r.stderr}`,
      );
    }
    expect(r.status).toBe(0);
  }, 120_000);

  it("AC-7: `pnpm --filter web typecheck` exits 0 (web consumes the workspace api-client)", () => {
    const r = runPnpm(["--filter", "web", "typecheck"]);
    if (r.status !== 0) {
      throw new Error(
        `web typecheck failed (status=${String(r.status)})\nSTDOUT:\n${r.stdout}\nSTDERR:\n${r.stderr}`,
      );
    }
    expect(r.status).toBe(0);
  }, 180_000);
});
