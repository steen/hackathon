import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: [
      "**/*.{test,spec}.?(c|m)[jt]s?(x)",
      "**/*_test.?(c|m)[jt]s?(x)",
    ],
    testTimeout: 120_000,
    hookTimeout: 120_000,
  },
});
