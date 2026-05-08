- **db**: add forward migration `0005_dms_and_read_state.sql` introducing
  `dm_conversations`, `dm_messages`, `channel_reads`, and `dm_reads` tables
  plus `last_message_id` / `last_message_at` columns on `channels`. Indexes
  follow the listing access paths (`idx_dm_messages_conv_created`,
  `idx_channel_reads_user`, `idx_channels_last_message_at`). Schema-only —
  no handlers populate the new rows yet (phase-9 sub-issues D, G1, H ship
  the writes).
- **repo**: extend `apps/server/internal/repo.Channel` with nullable
  `LastMessageID` / `LastMessageAt` pointer fields tagged `omitempty` so
  the HTTP wire shape stays unchanged until the populating sub-issue
  ships matching mirrors in `packages/go-client/channels.go` and
  `packages/api-client/src/types.ts`.
