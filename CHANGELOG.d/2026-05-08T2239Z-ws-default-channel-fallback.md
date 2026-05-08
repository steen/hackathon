### Added

- `apps/server/internal/wsapi`: L15 default-channel fallback per `specs/plans/phase-9/ws-routing.md`. A `/ws` upgrade with no `?channel=` now resolves to the seeded `general` channel id via the new `Config.DefaultChannelResolver` (production wiring) and subscribes to BOTH that channel topic AND `user:<viewer>`. Legacy callers (go-client, CLI, `chatd watch`) that historically opened `/ws` without `?channel=` no longer regress with HTTP 400. Closes #919.
