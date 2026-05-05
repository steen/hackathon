### Fixed

- `apps/web` Chat: a live message arriving while the user has scrolled up to read history no longer yanks the list back to the bottom. Auto-scroll-to-bottom now runs only when the user was already pinned within 8px of the bottom; otherwise their scroll position is preserved (#633, parent #156).
