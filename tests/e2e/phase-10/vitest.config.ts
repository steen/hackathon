import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  test: {
    include: [resolve(here, "*.test.ts")],
    testTimeout: 30_000,
    pool: "forks",
    poolOptions: { forks: { singleFork: true } },
  },
});
