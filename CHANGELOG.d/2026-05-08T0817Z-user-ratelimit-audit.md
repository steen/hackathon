### Changed

- Per-user channel-write 429s now append a row to `auth_events` with the
  rejected user id set, mirroring the per-IP rate-limit audit story
  (#883). Reuses the existing `rate_limited` event kind; the row's
  `user_id` column distinguishes per-user from per-IP rejections.
