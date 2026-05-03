---
feature: seed-general-channel
phase: phase-3
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: stub
total_acs: 3
covered: 0
partial: 0
missing: 0
deferred: 3
---

# E2E test analysis: Seed `#general` channel

**Spec:** `specs/plans/phase-3/30-feature-seed-general-channel.md`
**Implementation status:** stub — no `apps/server/internal/seed/` package, no `EnsureGeneralChannel` call from `apps/server/main.go`, no INSERT for a `general` channel in `migrations/0001_init.sql`. Verified by `ls /Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/server/internal/` (no `seed` dir; only `auth config db http hub ids ratelimit repo wsapi`), reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/migrations/0001_init.sql` (CREATE TABLE only, no INSERT), and reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/apps/server/main.go` (after `appdb.Apply` at line 86 the next call is `repo.New`, no seed step).
**Note:** The wsapi handler hardcodes a `defaultChannel = "#general"` sentinel (`apps/server/internal/wsapi/handler.go:18`) used as a fast-path bypass for the `ChannelLookup` check on `/ws` upgrade. This is wire-level legacy compatibility, NOT a database row — `GET /api/channels` against a fresh DB returns an empty list today. The spec is explicit that the seed must produce a real channel row visible to the channels API.
**E2E test directory:** `tests/e2e/phase-3/seed-general-channel/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | On first server boot (when no channels exist), a channel named `general` is created automatically. | deferred | — |
| AC-2 | Re-running the server does not create duplicates and does not error on the unique-name constraint. | deferred | — |
| AC-3 | The seed runs after migrations and before the HTTP listener starts accepting connections. | deferred | — |

## Findings

### Missing E2E tests

None — feature is stub.

### Deferred E2E tests

All 3 ACs deferred. When implementation lands, a single Go test file at `tests/e2e/phase-3/seed-general-channel/seed_test.go`:

- **AC-1 (seed creates on first boot):** boot the server fresh against an empty `CHAT_DB_PATH=<temp>` (random secret, random invite, random port). Wait for ready. Register a user and obtain a JWT (the channels API requires auth — see `apps/server/main.go` lines 139-147 where `ch.Routes(mux, require, msg)` wraps in `RequireJWT`). `GET /api/channels` with the bearer token. Assert response is `{"ok":true, "data":[...]}` with at least one channel whose `name == "general"` (NOT `#general` — the spec is explicit that the channel name is `general`; the `#` is a UI/wire-protocol artifact in the wsapi defaultChannel sentinel only).
  - Edge case worth pinning: assert the seeded channel's `id` is a valid 26-char Crockford-base32 ULID. The spec calls for a fresh ULID per implementation step 2; if it ships as a hardcoded constant the test should still pass against any valid ULID, but a malformed id (e.g. `"general"` reused as the id) would fail and is worth catching.
- **AC-2 (idempotent re-run):** boot the server, shut it down cleanly, boot it again against the SAME `CHAT_DB_PATH`. Second boot must not return non-zero and must not log `UNIQUE constraint failed: channels.name` (the typed error that `repo.CreateChannel` maps to `ErrChannelNameTaken` per the changelog entry from the channels-and-messages feature). Then `GET /api/channels` and assert exactly ONE channel named `general` exists (count = 1, not 2).
  - Implementation matters: the spec's seed function `EnsureGeneralChannel` should SELECT-then-INSERT (or rely on `INSERT OR IGNORE`). The test must catch the broken case where the seed always INSERTs and lets the unique constraint propagate.
- **AC-3 (seed runs before listener accepts):** the spec is explicit that seeding happens after migrations and before the HTTP listener accepts. The honest test is: as soon as the listener is reachable (first successful TCP dial to the bound port), `GET /api/channels` (after auth) must already show the seeded channel. There must be no observable window where the server accepts connections but the channel is absent.
  - Test shape: a tight loop — dial the port until it accepts, then immediately fire the channels GET. Repeat 20× (each repeat against a fresh boot + fresh DB) to flush out any race. If even one iteration sees an empty channel list, AC-3 fails.
  - This is the only AC where the order matters operationally — the spec calls it out specifically. Worth the extra test cost.

### Helpers and harness notes

- Seed tests need a SHELL-OUT to the built binary (not in-process `httptest.Server`) because AC-3 is about the order of `main.go`'s startup steps. An in-process test bypasses the listener bind step and can't observe the "is the seed visible at the moment connections are accepted" property.
- Reuse `startServer(t)`, `freePort(t)`, `randomSecret(t,32)`, `repoRoot(t)` from `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/tests/server-ws-hub/hub_test.go`. Extend `startServer` (or fork into this test file's helper) to also pass `CHAT_DB_PATH` so the auth/channels code paths are wired.
- Need a register+login helper to mint a bearer token. Pattern: `POST /api/auth/register` with the test invite code, parse the response token, use it in `Authorization: Bearer <token>` for the channels GET. There's no existing E2E helper for this in `tests/server-ws-hub/`; either inline it (~30 lines) or pull from `apps/cli/cmd/testserver_test.go` (referenced in the cli-full-commands findings doc) if that helper is exported. Quick check would be needed before relying on it.
- For AC-2, the test must shut the first server cleanly (SIGTERM, wait for the `srv.Shutdown` 5s timeout) before booting the second — otherwise the temp SQLite file may still be locked and the second boot fails for the wrong reason. `t.Cleanup` ordering matters; consider explicit `cmd.Process.Signal(syscall.SIGTERM); cmd.Wait()` rather than deferring the kill.

## Recommendations for /test-implement

1. Land the test file with all 3 cases as `t.Skip("seed not implemented — see specs/plans/phase-3/30-feature-seed-general-channel.md")`.
2. The spec calls out the seed as a one-function package (`apps/server/internal/seed/seed.go`). After the impl PR merges, the unit test `seed_test.go` in that package will cover AC-1 and AC-2 at the function level. Our E2E tests should NOT duplicate that — focus on the integration property: "after a real binary boot, the channel is visible via the public API." That's the value-add over the unit test.
3. AC-3 (timing/ordering) cannot be tested at the unit level — only at the integration level. This is the highest-priority test of the three.
4. Watch for a related drift: if a future feature changes the channel name from `general` to something else (e.g. drops the `#` prefix in the wsapi sentinel for consistency), this test should catch it. Pin the exact string `"general"` in the assertion.
