- **perf(channels)**: short-circuit `MaterializeChannelReadsTx` on the
  GET /api/channels hot path. After the first listing a viewer's
  `channel_reads` rows match the count of channels with a tip; the
  pre-check skips the BEGIN/COMMIT cycle for every subsequent listing
  rather than running a zero-row INSERT. Materialization semantics are
  unchanged (per-channel pin to `last_message_id`). Closes #937.
