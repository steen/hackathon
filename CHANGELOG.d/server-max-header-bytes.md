### Security

- **server**: cap `http.Server.MaxHeaderBytes` at 16 KiB (default 1 MiB) to bound the slow-header DoS surface paired with `ReadHeaderTimeout`. Refs #78. (2026-05-03T18:42Z)
