import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.spec.ts"],
    // Boots a real server + spawns CLI binaries; needs slack on slow CI.
    testTimeout: 60_000,
    hookTimeout: 60_000,
    // Each spec owns a fresh server fixture; running serially keeps log output
    // per-scenario readable when something breaks.
    pool: "forks",
    poolOptions: { forks: { singleFork: true } },
  },
});
