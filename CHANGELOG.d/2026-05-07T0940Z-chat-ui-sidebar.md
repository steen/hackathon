### Changed

- `Sidebar` shell extracted from `apps/web/src/routes/Chat.tsx` into `@hackathon/chat-ui`. Preserves `<aside aria-label="Chat sidebar">` plus a `header` slot that consumers wire (Chat.tsx still owns the username + Sign-out controls; `AuthContext` does not bleed into chat-ui).
