### Fixed

- `apps/server/internal/http`: cap `auth_events.username` at 64 bytes on insert inside `LogAuthEvent`. PR #723 wrote the attempted username from request bodies into the audit column, bounded only by SEC-7's 16KB cap, so a user pasting their password into the username field would land plaintext into the audit log. The cap is enforced in the store, so the `LogRateLimited` middleware path inherits the same bound.
