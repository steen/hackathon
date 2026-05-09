import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    // libsodium-wrappers-sumo ships a ~500 KB minified loader plus
    // ~200 KB of WASM glue (the sumo build is required for Argon2id;
    // standard libsodium-wrappers omits crypto_pwhash). Vite's default
    // 500 KB chunk-size warning would fire on every CI build; bump the
    // threshold so genuine chunk-bloat regressions still surface. The
    // observed sumo-bundled output sits around 730 KB minified.
    chunkSizeWarningLimit: 800,
  },
  resolve: {
    alias: {
      "@hackathon/api-client": fileURLToPath(
        new URL("../../packages/api-client/src/index.ts", import.meta.url),
      ),
      "@hackathon/chat-ui": fileURLToPath(
        new URL("../../packages/chat-ui/src/index.ts", import.meta.url),
      ),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: true },
      // No changeOrigin on /ws: keep Host as localhost:5173 so coder/websocket's
      // default Host-vs-Origin same-origin check passes without forcing devs to
      // also set CHAT_ALLOWED_ORIGINS=http://localhost:5173.
      "/ws": { target: "http://127.0.0.1:8080", ws: true },
    },
  },
});
