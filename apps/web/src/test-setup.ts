import "@testing-library/jest-dom/vitest";

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve as resolvePath } from "node:path";

// vitest+vite returns an empty module for `import "./styles.css"` in jsdom,
// so getComputedStyle can't resolve selectors that the stylesheet defines
// (e.g. `.msg--pending .msg__body { font-style: italic }`). Read the file
// off disk once and attach it as a permanent <style> tag — every test
// gets jsdom CSSOM rules without each test re-implementing the workaround.
const here = dirname(fileURLToPath(import.meta.url));
const cssText = readFileSync(resolvePath(here, "styles.css"), "utf-8");
const styleEl = document.createElement("style");
styleEl.dataset.testInjected = "global-styles";
styleEl.textContent = cssText;
document.head.appendChild(styleEl);
