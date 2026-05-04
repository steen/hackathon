### Changed

- Server: `POST /api/channels/{id}/messages` now emits `code: "message_too_large"` instead of generic `bad_request` when a body exceeds the 4 KiB cap, letting clients distinguish a body-cap reject from any other 400. Empty/whitespace-only bodies remain `bad_request`. (#501, closes #419)
