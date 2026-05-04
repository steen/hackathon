### Changed

- Web message list now humanizes timestamps. Today renders as `HH:MM` (24h), within the last 6 days as `Wkd HH:MM` (e.g. `Sun 23:50`), older as `Mon D HH:MM`. The `<time dateTime={iso}>` attribute keeps the precise RFC3339 value for screen readers and scrapers; only the visible label changes.
