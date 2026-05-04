import { readFileSync } from "node:fs";
import { resolve as resolvePath } from "node:path";
import "@testing-library/jest-dom/vitest";

// vitest+vite returns an empty module for `import "./styles.css"` under
// jsdom, so getComputedStyle can't resolve any rule. Inject the real
// stylesheet once per test run so CSS-dependent assertions inherit the
// fix without per-test workarounds. The path is resolved against
// process.cwd() (the apps/web package root, where vitest is invoked).
const cssText = readFileSync(resolvePath(process.cwd(), "src/styles.css"), "utf-8");
const styleEl = document.createElement("style");
styleEl.dataset.testInjected = "app-styles";
styleEl.textContent = cssText;
document.head.appendChild(styleEl);
