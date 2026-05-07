### Changed

- `ConnectionBadge` now lives in `@hackathon/chat-ui` instead of inline in `apps/web/src/routes/Chat.tsx`. The canonical `ConnectionStatus` type ships from chat-ui as well; `apps/web/src/hooks/useMessages.ts` keeps a deprecated `ConnectionState` alias for one cycle so existing imports compile.
