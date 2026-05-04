### Fixed

- `apps/server/internal/wsapi`: reorder `/ws` upgrade so the channel-existence check (`Config.ChannelLookup`) runs **before** `TicketStore.Redeem`. A typo or probe targeting an unknown channel now returns 404 without burning the one-shot ticket. The `#general` legacy sentinel still skips the lookup (audit #78, low-severity).
