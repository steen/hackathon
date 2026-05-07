### Fixed

- Prettier formatting on six files touched during the Phase 6 extraction (`Chat.tsx`, `MessageItem.tsx`, `MessageList.css`, `MessageList.tsx`, `TopBar.tsx`, `chat-ui types.ts`). All matched files now pass `prettier --check`.
- Go lint regression in `apps/server/internal/http/users_handlers.go`: `defer rows.Close()` now wraps the unchecked error in a closure (matches the sibling pattern in `presence_handlers.go`).
- Go lint regression in `tests/e2e/phase-2/web-app/auth_screens_test.go`: renamed `chatUiSrc` → `chatUISrc` to satisfy `revive`'s var-naming rule.
