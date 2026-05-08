### Server

- `repo.InsertMessage` is now `repo.InsertMessageTx`: each channel-message
  insert runs inside a `BeginTx` block that also `UPDATE`s the owning
  channel's denormalized `last_message_id` / `last_message_at`. The
  channels listing can now derive unread counts without a per-row scan
  of `messages` (decision log L11 + L21; cold-pass SC3). Mirrors the
  `BeginTx` pattern in `apps/server/internal/http/auth_store.go`.
