### Fixed

- `validULID` now upper-folds Crockford-base32 input so a lowercase
  cursor or `peer_user_id` matches server-issued (uppercase) ULIDs under
  SQLite's BINARY collation. Previously a lowercase `before=` cursor
  silently returned the full message history (every uppercase id sorts
  below any lowercase value), and a lowercase `peer_user_id` 404'd from
  POST /api/dms despite the equivalent uppercase id existing. Both DM
  and channel-message list endpoints now feed the normalized cursor
  into the repo. (#934)
