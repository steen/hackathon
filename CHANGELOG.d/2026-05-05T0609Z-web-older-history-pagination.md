### Added

- Chat surfaces a "Load older messages" button at the top of the message list once the initial history page returns full (50 rows). Clicking it pages the channel backwards via `listMessages(channelId, { before: <oldest-visible-ulid>, limit: 50 })`, reverses the response, and prepends it above the existing rows; the trigger hides once an older page comes back short. Honors the prepend contract from #132 — oldest-of-block at the new top, newest-of-block immediately above the previous-top row, dedup by id. (#588)
