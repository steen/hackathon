import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AuthProvider } from "./auth/AuthContext.js";
import { App } from "./App.js";
import "./styles.css";

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
