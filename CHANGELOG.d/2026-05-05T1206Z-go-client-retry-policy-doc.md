### Changed

- `packages/go-client`: documented the retry/backoff contract — the client is fire-once by design, callers own retry policy (use `context` for deadlines and wrap `WithHTTPClient` with a retrying `RoundTripper` if needed). No code change; the typed surface and zero-value defaults are unchanged.
