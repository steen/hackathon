### Fixed

- WS `?channel=<id>` now upper-folds non-sentinel channel ids through the same
  normalizer as `/api/channels/{id}/messages`, so a lower-case ULID resolves
  the same channel on both surfaces. The `#general` sentinel is unchanged
  (audit #78, info).
