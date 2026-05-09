### Added

- `goclient.Client.ListUsers(ctx) ([]UserSummary, error)` — issues `GET /api/users` via the existing envelope helper and decodes the canonical `{users:[{id,username},...]}` response. Mirrors `apps/server/internal/http/users_handlers.go::UserSummary`; error mapping uses the standard `*APIError` (e.g. `unauthorized` on a missing/invalid token).

### Changed

- `apps/cli/cmd/dm.go::resolvePeer` now calls `client.ListUsers` instead of a local raw-`net/http` `fetchUsers` helper. The obsolete `token` parameter is removed from `resolvePeer`; the local `userSummary`/`usersListResponse` types and the `bytes`/`net/http` imports are gone.
