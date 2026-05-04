import { defineWorkspace } from "vitest/config";

// Each entry is a vitest project. Vitest auto-loads this file when run
// with no --workspace flag from this directory, so
// `pnpm -r --if-present test` discovers every feature suite.
//
// `extends` points at the feature's own vitest.config.ts so include
// patterns, globalSetup, and timeouts stay owned by the feature; the
// only thing this file adds is a stable project `name` so per-feature
// `e2e:<feature>` scripts in package.json can scope to one suite via
// --project=<name>.
//
// Adding a new suite: append one entry below pointing at
// phase-N/<feature>/vitest.config.ts with a unique name, then add an
// `e2e:<feature>` script that runs `vitest run --project=<name>`.
export default defineWorkspace([
  {
    extends: "./vitest.config.ts",
    test: { name: "cli" },
  },
  {
    extends: "./phase-2/ts-api-client-package/vitest.config.ts",
    test: { name: "ts-api-client-package" },
  },
]);
