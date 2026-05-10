### Changed

- `repo.CreateChannel` (non-Tx) now maps `channels.id` PRIMARY KEY trips to `ErrChannelIDTaken`, matching `CreateChannelTx`. Either entrypoint surfaces the same `(name, id)` sentinel pair so handlers don't have to branch on driver-specific error prose. (#1028)
