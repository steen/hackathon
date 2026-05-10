### Changed

- `apps/server/internal/http/members_handlers.go`: drop the unused `callerBoxPub` binding in the public-channel auto-fill invite path. The caller's box-pubkey was fetched from `lookupUserPubkeys` and immediately discarded with `_ = callerBoxPub`; the auto-fill row only pins the caller's sign-pubkey. Closes #1005.
