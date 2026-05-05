### Security

- **server**: implement the `CHAT_TRUSTED_PROXY` parser (PRD §9 / §11). When set to `1`, the access-log `remote_ip` field, the auth-event audit IP, and the per-IP login + register rate-limit bucket key all honor the leftmost `X-Forwarded-For` entry; the value is validated via `netip.ParseAddr` and rejected entries fall back to `r.RemoteAddr` so a client cannot poison the access log. Default (unset or any other value) is unchanged: trust only `r.RemoteAddr`. Closes #650. (2026-05-05T09:44Z)
