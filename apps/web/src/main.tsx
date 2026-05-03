import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { WebSocketClient, type WSConnectionState } from "@hackathon/api-client";
import { AuthProvider } from "./auth/AuthContext.js";
import { App } from "./App.js";
import "./styles.css";

// E2E hook: record every WS connection-state transition on `window.__chatd`
// so Playwright can assert the open→closed→connecting→open sequence
// directly. The DOM badge (Chat.tsx) collapses fast transients on a quick
// reconnect, which is what made the WS-drops e2e flaky (#110).
declare global {
  interface Window {
    __chatd?: { wsTransitions: WSConnectionState[] };
  }
}
window.__chatd = { wsTransitions: [] };
WebSocketClient.observe((state) => {
  window.__chatd?.wsTransitions.push(state);
});

const rootEl = document.getElementById("root");
if (rootEl === null) {
  throw new Error("missing #root element");
}

createRoot(rootEl).render(
  <StrictMode>
    <AuthProvider>
      <App />
    </AuthProvider>
  </StrictMode>,
);
