### Changed

- `wsapi.writeErrorFrame` now takes a named `wsapi.ErrCode` for the `code`
  parameter instead of a bare `string`. The existing `ErrCodeBodyTooLarge`
  and `ErrCodeRateLimited` constants are retyped to `ErrCode`, so call sites
  can no longer pass a typo'd literal as the wire code (#741).
