import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  test: {
    include: [resolve(here, "*.test.ts")],
    globalSetup: [resolve(here, "globalSetup.ts")],
    testTimeout: 60_000,
    hookTimeout: 90_000,
    pool: "forks",
    poolOptions: { forks: { singleFork: true } },
  },
});
