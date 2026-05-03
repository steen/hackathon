### Security

- **server**: log a startup `WARN` when `CHAT_ALLOW_PUBLIC_BIND=1` is set, since the deferred `CHAT_TRUSTED_PROXY` parser (PRD §9) is not yet wired and `clientIP` falls back to `RemoteAddr`; behind a reverse proxy this collapses per-IP rate-limit buckets onto the proxy IP. Refs #78. (2026-05-03T18:59Z)
