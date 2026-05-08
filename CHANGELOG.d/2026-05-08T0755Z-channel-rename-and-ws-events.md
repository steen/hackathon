### Added

- `PATCH /api/channels/{id}` rename endpoint. Returns 200 + the updated
  channel envelope on success; 400 on invalid name, 403 when the target
  is the seeded `#general` channel, 404 on unknown id, 409 on duplicate
  name, 429 when the per-user channel-write bucket is empty.
- Per-user channel-write rate limiter on POST + PATCH `/api/channels`
  (default burst 10, refill 1m). New env vars `CHAT_CHANNEL_WRITE_BURST`
  and `CHAT_CHANNEL_WRITE_REFILL` override the production defaults; a
  startup `WARN` logs whenever either differs from the default.
- Outbound WS frame
  `{"type":"channel","data":{"kind":"create"|"rename","channel":{...}}}`
  broadcast via `Hub.BroadcastAll` on every successful channel create or
  rename. Failure paths (400 / 403 / 404 / 409 / 429 / 500) emit no
  frame.
