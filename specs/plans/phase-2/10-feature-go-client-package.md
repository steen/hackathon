# Feature: `packages/go-client` (HTTP + WS client)

**Parent phase:** [Phase 2: Web UI + shared clients](../phase-2-web-ui-shared-clients.md)
**Status:** planned

## Requirements covered
- (foundation for US-8; the CLI in `20-feature-cli-full-commands.md` consumes this package)

## Acceptance criteria
- A reusable Go package at `packages/go-client` (part of the single-root `hackathon` module, imported as `hackathon/packages/go-client`) exposes typed methods for: `Login`, `Register`, `Me`, `Logout`, `ListChannels`, `CreateChannel`, `ListMessages`, `PostMessage`, `WsTicket`, and `Watch` (returns a stream of inbound events).
- The client handles base URL, auth token storage (in memory), and JSON/error-envelope decoding.
- The client is consumable from `apps/cli` via a normal in-module import (no workspace replace directive needed).

## Implementation steps
1. Create `packages/go-client/client.go` with `New(baseURL, opts...)` returning a `*Client`. No `go.mod` — the package lives inside the existing single-root `hackathon` module.
2. Add typed request/response structs mirroring the server JSON.
3. Implement REST methods using `net/http`, returning typed errors derived from the error envelope.
4. Implement `WsTicket` then `Watch(ctx, channelID)` which redeems the ticket and returns a `<-chan Event`.
5. Add a small `Token(t string)` setter and `WithBearer` round-tripper.

## Test plan
- Unit tests against `httptest.Server` for each REST method (success + error envelope).
- Integration test: `Watch` receives an event posted via `PostMessage`.

## Files expected to be touched or created
- `packages/go-client/client.go`
- `packages/go-client/auth.go`
- `packages/go-client/channels.go`
- `packages/go-client/messages.go`
- `packages/go-client/ws.go`
- `packages/go-client/*_test.go`

## Risks
- API drift between the server and client; mitigated by sharing types via this package and updating both in lockstep.
