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

const cssBundle = [
  resolvePath(here, "styles.css"),
  // Component CSS lives in @hackathon/chat-ui after the Phase 6 extraction;
  // bundle every component stylesheet here so the same getComputedStyle
  // selectors keep resolving in jsdom without per-test workarounds.
  resolvePath(here, "../../../packages/chat-ui/src/tokens.css"),
  resolvePath(here, "../../../packages/chat-ui/src/ChannelsList/ChannelsList.css"),
  resolvePath(here, "../../../packages/chat-ui/src/ChannelHeader/ChannelHeader.css"),
  resolvePath(here, "../../../packages/chat-ui/src/ConnectionBadge/ConnectionBadge.css"),
  resolvePath(here, "../../../packages/chat-ui/src/MessageComposer/MessageComposer.css"),
  resolvePath(here, "../../../packages/chat-ui/src/MessageList/MessageList.css"),
  resolvePath(here, "../../../packages/chat-ui/src/PresenceList/PresenceList.css"),
  resolvePath(here, "../../../packages/chat-ui/src/Sidebar/Sidebar.css"),
  resolvePath(here, "../../../packages/chat-ui/src/TopBar/TopBar.css"),
]
  .map((p) => readFileSync(p, "utf-8"))
  .join("\n");
const styleEl = document.createElement("style");
styleEl.dataset.testInjected = "global-styles";
styleEl.textContent = cssBundle;
document.head.appendChild(styleEl);
