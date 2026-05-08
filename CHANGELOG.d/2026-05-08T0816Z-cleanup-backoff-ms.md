### Refactor

- **web**: drop the unused `BACKOFF_MS` export from `apps/web/src/hooks/useMessages.helpers.ts`. PR #881 lifted the WS lifecycle into `useChatSocket.ts`, which now owns the canonical `BACKOFF_MS` re-exported through `useMessages.ts`; the helpers copy was left behind to keep that diff narrow. No user-visible change — reconnect cadence is unchanged. (#885)
