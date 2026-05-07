- Wired `CHAT_BCRYPT_COST`: parsed at startup via `config.ParseBcryptCost`,
  applied to `auth.BcryptCost` through `auth.SetBcryptCost`. Defaults to
  10 (PRD §9 / OWASP floor); accepts `[10, 31]`. Empty, non-numeric, or
  out-of-range values abort boot with an error naming the env var.
  Existing stored hashes still verify against the cost embedded in each
  hash, so raising the cost does not break old logins. Closes #785.
