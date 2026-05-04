### Added

- `apps/server/internal/http`: emit a `register_failed` row in `auth_events` when a `/api/auth/register` attempt is rejected (invalid invite code, bad username, weak password, hash failure, conflict). Closes the PRD §9 audit-coverage gap flagged during PR #185 review (#195).
