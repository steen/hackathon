## Phase 10 — wire the real `current_generation_id` on `members_changed` from invite/kick (#1006)

- `MembersHandlers.broadcastMembersChanged` now reads
  `MAX(channel_keys.generation_id)` live and joins
  `channel_members` ⨝ `users.username` to populate
  `members_at_rotation`, replacing the placeholder
  `current_generation_id: 1` + empty array used while
  `membersChangedFrame` was landing in #984. Channels with no wrap
  rows yet (legacy bootstrap path) fall back to `creatorBootstrapGenID`
  so the wire never exposes a zero generation. Repo / username lookup
  errors fan out an empty `members_at_rotation` rather than dropping
  the frame — subscribers still see the cache-bust signal.
- Frame is built via the existing `membersChangedFrame` helper from
  `apps/server/internal/http/ws_events.go` (#984), so the keys-RPC
  rotation arm and the invite/kick arm now share one builder.
