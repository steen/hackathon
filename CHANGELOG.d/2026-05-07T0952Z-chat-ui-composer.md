### Changed

- `MessageComposer` extracted from `apps/web/src/routes/Chat.tsx` into `@hackathon/chat-ui`. Internalizes the IME composition flag, Enter / Shift+Enter logic, byte counter (>=80% threshold + over-cap `aria-invalid`), and `composer__counter` chrome. Send button styled as a rounded purple block via `--accent`.
- `byteLength` helper inlined into the component (no longer in Chat.tsx).
