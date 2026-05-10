### Changed

- `apps/server/internal/http/members_handlers.go`: remove the `if !hasGen { currentGen = creatorBootstrapGenID }` fallback on the wrap-carrying invite path. With #984's bootstrap-mode `POST /api/channels/{id}/keys` merged, a private channel with no key generation on file is no longer reachable in correct flows; the handler now returns `400 bad_request` so a misconfigured caller cannot accumulate gen-1 wraps for invitees without an L30-signed creator wrap on file (#1014).
