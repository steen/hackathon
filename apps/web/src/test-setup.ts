import "@testing-library/jest-dom/vitest";

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve as resolvePath } from "node:path";

// vitest+vite returns an empty module for `import "./styles.css"` in jsdom,
// so getComputedStyle can't resolve selectors that the stylesheet defines
// (e.g. `.msg--pending .msg__body { font-style: italic }`). Inline the
// app's stylesheet plus chat-ui's barrel `styles.css` (which @imports every
// component CSS file) and attach as a permanent <style> tag.
//
// `@import` rules nested inside an injected <style> tag don't trigger
// network fetches in jsdom and silently no-op, so unwrap them: read each
// referenced file and concatenate. This keeps the path math in one place
// instead of every consumer rebuilding the file list.
const here = dirname(fileURLToPath(import.meta.url));
const chatUiSrc = resolvePath(here, "../../../packages/chat-ui/src");

function readBarrel(barrel: string, baseDir: string): string {
  const text = readFileSync(barrel, "utf-8");
  const lines = text.split("\n");
  const out: string[] = [];
  for (const line of lines) {
    const m = /^@import\s+"([^"]+)";?\s*$/.exec(line);
    if (m === null) {
      out.push(line);
      continue;
    }
    const target = resolvePath(baseDir, m[1] ?? "");
    out.push(readFileSync(target, "utf-8"));
  }
  return out.join("\n");
}

const cssBundle = [
  readBarrel(resolvePath(here, "styles.css"), here),
  readBarrel(resolvePath(chatUiSrc, "styles.css"), chatUiSrc),
].join("\n");

const styleEl = document.createElement("style");
styleEl.dataset.testInjected = "global-styles";
styleEl.textContent = cssBundle;
document.head.appendChild(styleEl);
