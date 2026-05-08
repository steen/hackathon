### Added

- DM read state: `POST /api/dms/{id}/read` advances the recipient's
  `dm_reads.last_read_dm_message_id` (advance-only per decision-log L5;
  posting an older `message_id` is a silent no-op). Returns `204`. The
  endpoint enforces the participant ACL (`L8` — non-participants get
  `404`, no membership leak) and the per-user `read-mark` token bucket
  (`L17`, default `Burst=50 / Refill=1m`). On success the server emits
  a `{type:"read", scope:"dm"}` WebSocket frame to the caller's
  `user:<viewer>` topic for cross-device sync (no peer fan-out — `L10`).
