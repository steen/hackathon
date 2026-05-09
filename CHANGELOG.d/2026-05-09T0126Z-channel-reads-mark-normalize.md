### Fixed

- `POST /api/channels/{id}/read` now persists the upper-folded ULID
  returned by `validULID` instead of the raw request body. Previously
  a lowercase `message_id` landed lowercase in
  `channel_reads.last_read_message_id`, and SQLite's BINARY collation
  then sorted every uppercase row in the unread-count subquery below
  the cursor — inflating `unread_count` for the viewer until they
  re-marked with an uppercase id. The two sibling handlers
  (`dms_handlers.Mark`, messages-list `before=` cursor) were already
  normalized in #945; this is the third caller in the same package.
  (#947)
