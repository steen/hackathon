### Changed

- `apps/server`: drop the `COALESCE(r.last_read_message_id, '')` belt in the `unread_count` subquery of `ListChannelsWithReadState`. After the §11 materialize-on-listing pass, every non-empty channel has a `channel_reads` row for the viewer with non-NULL `last_read_message_id`, so `m.id > r.last_read_message_id` is well-defined for any channel that could contribute a non-zero count; never-messaged channels keep falling through the LEFT JOIN with `last_read_message_id` NULL and a structurally-zero count. Wire shape unchanged. Issue #938.
