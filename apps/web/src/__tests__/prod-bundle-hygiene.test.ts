import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, rmSync } from "node:fs";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// Regression guard for #699 / #693: the `window.__chatd` WS-transition test
// hook is gated on `import.meta.env.MODE !== "production"` in
// apps/web/src/main.tsx so the symbol does not ship in real prod bundles.
// This test runs the prod build and asserts no built artifact under
// dist/assets/*.js contains the literal `__chatd`. A future commit that
// drops the gate (or renames it incorrectly) will fail here.
//
// Slow (full Vite build, ~5–10s), so skipped by default. Enable with
// RUN_PROD_BUNDLE_HYGIENE=1, which CI sets on the pnpm test step.

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolvePath(here, "..", "..");
const distAssets = resolvePath(webRoot, "dist", "assets");

const enabled = process.env.RUN_PROD_BUNDLE_HYGIENE === "1";
const describeIfEnabled = enabled ? describe : describe.skip;

describeIfEnabled("test_web_prod_bundle_excludes_chatd_hook", () => {
  it("dist/assets/*.js after `pnpm run build` contains no `__chatd` literal", () => {
    rmSync(resolvePath(webRoot, "dist"), { recursive: true, force: true });

    execFileSync("pnpm", ["run", "build"], {
      cwd: webRoot,
      stdio: "inherit",
      env: { ...process.env, NODE_ENV: "production" },
    });

    const jsFiles = readdirSync(distAssets).filter((name) => name.endsWith(".js"));
    expect(jsFiles.length).toBeGreaterThan(0);

    const hits: { file: string; matches: number }[] = [];
    for (const name of jsFiles) {
      const text = readFileSync(resolvePath(distAssets, name), "utf-8");
      const matches = text.split("__chatd").length - 1;
      if (matches > 0) {
        hits.push({ file: name, matches });
      }
    }

    expect(
      hits,
      `prod bundle leaks the __chatd test hook — check the MODE gate in apps/web/src/main.tsx (#693, #699)`,
    ).toEqual([]);
  }, 120_000);
});
