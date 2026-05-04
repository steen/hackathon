### Fixed

- web/auth: AuthContext error banner no longer echoes raw `Error.message` (or non-`Error` rejection values) verbatim. Errors are now mapped through a small classifier (`ApiError` 401/403, 408, 5xx; `AbortError`/`TimeoutError`; `TypeError` for fetch network failures) to a closed set of curated banner strings, with the raw error logged to `console.error` for devtools diagnosis. Closes #191.
