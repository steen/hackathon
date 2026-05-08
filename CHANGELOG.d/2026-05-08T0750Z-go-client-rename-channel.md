### Added

- `goclient.Client.RenameChannel(ctx, id, name) (Channel, error)` — issues `PATCH /api/channels/{id}` and unwraps the standard envelope; error mapping mirrors `CreateChannel` (400/403/404/409/429/500 → typed `*APIError` with the server's `code`).
- `goclient.EventTypeChannel` constant and `goclient.ChannelEvent{Kind, Channel}` type for the new `{type:"channel",data:{kind,channel}}` WS frame. `Event.Channel` is populated by `decodeEvent` when `Type == "channel"`; malformed `data` falls through with `Channel` nil and the original bytes in `Raw`.
