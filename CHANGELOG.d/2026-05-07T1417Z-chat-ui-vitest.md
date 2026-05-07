- Added Vitest unit-test coverage to `packages/chat-ui` for the four
  state-bearing components — `MessageList`, `MessageItem`,
  `MessageComposer`, `TopBar`. 36 cases across 4 files exercise empty
  / loading / error states, day-divider boundaries, optimistic-send
  status transitions, sender-color determinism, IME-composition Enter
  guarding, byte-limit warning + over-cap gating, and the consolidated
  identity / connection surface introduced in Phase 6. Playwright
  remains the regression guard; this lets chat-ui iterate in
  isolation. Closes #808.
