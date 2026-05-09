### Fixed

- `POST /api/channels/{id}/read` now emits the contract-shaped `{type:"read"}` WS frame: `{type, data:{scope:"channel", target_id, last_read_message_id, unread_count}}` matching `specs/plans/phase-9/read-state.md` and the DM arm. Previously the channels arm shipped a flat `{type, scope, scope_id, last_read_message_id}` envelope without `unread_count`, forcing clients to maintain two decoders. (#942)
