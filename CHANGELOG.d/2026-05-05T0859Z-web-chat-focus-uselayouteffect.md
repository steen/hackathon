### Changed

- Chat focus delivery now uses `useLayoutEffect` (lands focus before paint, no body→anchor flash for screen-reader users) and re-runs when `activeChannel` resolves after first paint, promoting focus from the heading placeholder to the composer once `useChannels` resolves. A `composerFocusedRef` guard prevents subsequent state changes from stealing focus back if the user has tabbed elsewhere.
