### Fixed

- web: switch `.chat-layout` height from `100vh` to `100dvh` (with a `100svh` fallback for browsers that do not yet recognise `dvh`). On iOS Safari, `100vh` includes the soft-keyboard region, which pushed the composer below the visible viewport whenever the keyboard opened; the dynamic-viewport units track the visible area instead. (#618)
