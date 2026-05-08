### Added

- `packages/api-client`: `Client.renameChannel(id, name)` and `HttpClient.renameChannel(id, name)` issue `PATCH /api/channels/{id}` with `{name}` and unwrap the standard envelope; 4xx/5xx surface as the same typed `ApiError` shape used by `createChannel`. The exported `Event` union gains a `ChannelEvent` variant (`{type:"channel", data:{kind:"create"|"rename", channel}}`) so consumers can narrow `decodeFrame` output without casting; `decodeFrame` itself is unchanged. Closes #840. (2026-05-08T07:44Z)
