### Added

- Web app surfaces presence/channels/messages hook failures via a shared
  app-error sink (`apps/web/src/lib/userFacingError.ts` —
  `reportAppError` / `dismissAppError` / `useAppError`). `usePresence`,
  `useChannels`, and `useMessages` dispatch their curated banner copy into
  the sink on REST or WS faults; the App-shell `ErrorBanner` reads both
  the AuthContext error (session-restore wins) and the sink. Single-slot
  semantics match the existing one-message banner UX. (#594, closes #159)
