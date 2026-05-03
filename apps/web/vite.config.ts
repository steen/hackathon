import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@hackathon/api-client": fileURLToPath(
        new URL("../../packages/api-client/src/index.ts", import.meta.url),
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
