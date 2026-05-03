# Feature: Seed `#general` channel

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** planned

## Requirements covered
- (UX polish; ensures a fresh install has somewhere to talk; supports the demo flow)

## Acceptance criteria
- On first server boot (when no channels exist), a channel named `general` is created automatically.
- Re-running the server does not create duplicates and does not error on the unique-name constraint.
- The seed runs after migrations and before the HTTP listener starts accepting connections.

## Implementation steps
1. Add `apps/server/internal/seed/seed.go` with `EnsureGeneralChannel(ctx, store)`.
2. The function checks if a channel named `general` exists; if not, inserts one with a fresh ULID.
3. Call `EnsureGeneralChannel` from `main.go` after migrations.

## Test plan
- `test_seed_creates_general_when_absent` — covers seed correctness.
- `test_seed_is_idempotent` — covers re-run safety.

## Files expected to be touched or created
- `apps/server/internal/seed/seed.go`
- `apps/server/internal/seed/seed_test.go`
- `apps/server/main.go` (call seed)

## Risks
- None identified.
