### Removed

- Backwards-compat aliases & fallbacks introduced during Phase 6:
  - `apps/web/src/hooks/useMessages.ts`: drop the `ConnectionState` and `MessageStatus` aliases. Direct imports from `@hackathon/chat-ui` only; internal `connection` field uses the canonical name.
  - `apps/web/src/hooks/usePresence.ts`: drop the `.catch(() => ({ users: [] }))` around `GET /api/users`. The endpoint exists in this branch; failures should surface, not be swallowed.
  - `apps/web/src/routes/Chat.tsx`: drop the `IS_AT_BOTTOM_TOLERANCE_PX` re-export. `Chat.test.tsx` imports the constant directly from chat-ui.

This project is a hackathon repo, not deployed; no consumers exist for the legacy aliases. Removing them keeps the import surface minimal.
