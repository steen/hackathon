- **db**: add forward migration `0002_messages_cursor_index.sql` creating
  `idx_messages_channel_id ON messages(channel_id, id)` so paginated history
  (`WHERE channel_id = ? AND id < ?`) no longer relies on the implicit PK
  index for its query plan. Addresses the info-severity finding in #78.
