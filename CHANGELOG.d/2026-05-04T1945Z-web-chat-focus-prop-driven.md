### Changed

- Post-signin focus delivery moved from imperative `document.querySelector` lookups in `App.tsx` to ref-driven targets owned by `<Chat />`. Composer textarea, channel-name heading, and message-list container expose refs; a `useEffect` in `Chat` focuses them in priority order on mount. Behavior (composer when enabled, else heading, else list; logout returns to Login) unchanged.
