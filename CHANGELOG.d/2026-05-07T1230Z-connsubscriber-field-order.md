### Changed

- Reordered `connSubscriber` fields in `apps/server/internal/wsapi/handler.go` so `closeMu` sits immediately above the channels it guards (`done`, `shutdown`, `closeFlush`). Read-only fields (`userID`, `channel`) and the unguarded `send` channel are split onto separate blank-line-delimited groups so the lock-protection scope is visible at a glance. Code-only refactor; zero behavior diff.
