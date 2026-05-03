---
feature: go-client-package
phase: phase-2
analyzed_at: 2026-05-03T19:55:35Z
analyzed_commit: ff5576d7892382c8a680185251d43e8f9c8554b4
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: `packages/go-client` (HTTP + WS client)

**Spec:** `specs/plans/phase-2/10-feature-go-client-package.md`
**Implementation status:** implemented — `packages/go-client/{client,auth,channels,messages,ws}.go` ship the full surface (REST methods + ticket-redeemed `Watch`) under `package goclient`, importable as `hackathon/packages/go-client`. 23 in-package tests across the five `*_test.go` files pass at this SHA.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | A reusable Go package at `packages/go-client` (part of the single-root `hackathon` module, imported as `hackathon/packages/go-client`) exposes typed methods for: `Login`, `Register`, `Me`, `Logout`, `ListChannels`, `CreateChannel`, `ListMessages`, `PostMessage`, `WsTicket`, and `Watch` (returns a stream of inbound events). | covered | All 10 methods present and tested at the public boundary. `auth_test.go` covers `Login`/`Register`/`Me`/`Logout`/`WsTicket`; `channels_test.go` covers `ListChannels`/`CreateChannel`; `messages_test.go` covers `ListMessages`/`PostMessage`; `ws_test.go` covers `Watch`. Package is in the single-root module — confirmed by absence of any `packages/go-client/go.mod` and the test `package goclient` declaration. |
| AC-2 | The client handles base URL, auth token storage (in memory), and JSON/error-envelope decoding (envelope shape is `{ok, data, error: {code, message}}` per `apps/server/internal/http/errors.go`). | covered | `client_test.go::TestNewTrimsTrailingSlash` (base URL normalization) + `TestSetTokenRoundTrip` + `TestWithTokenOption` (in-memory token storage with sync.RWMutex per `client.go:41-43`) + `TestDoSurfacesAPIError` (envelope `{ok:false, error:{code,message}}` decoded into `*APIError` with `Status`, `Code`, `Message` fields per `client.go:95-103`) + `TestDoEmptyBodyOnError` (graceful handling when an upstream proxy returns a 5xx with empty body — returns typed `APIError{Code:"unknown"}` rather than a JSON parse error, so callers can branch on Status). |
| AC-3 | HTTP requests authenticate with `Authorization: Bearer <jwt>`; WebSocket connections use the one-shot ticket flow — call `WsTicket` to mint a ticket, then redeem it on upgrade as `?ticket=<hex>` (see `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`). Bearer tokens are not sent on the WS upgrade. | covered | `client_test.go::TestDoSendsBearerWhenSet` (verifies `Authorization: Bearer <token>` is on outgoing REST requests) + `TestDoOmitsBearerWhenUnset` (no header when token is empty). For the WS upgrade: `ws_test.go::TestWatchEndToEnd` drives the full ticket-then-upgrade flow against a real `httptest.Server` running the same `wsapi.Handler`, asserting only `?ticket=<hex>` is present on the upgrade URL. The bearer-not-on-upgrade contract is structural in `ws.go:46-67`: `Watch` builds the URL via `buildWSURL(c.baseURL, ticket.Ticket, opts.ChannelID)` and dials directly with `websocket.Dial` — no `Authorization` header is set on the dial options. |
| AC-4 | The client is consumable from `apps/cli` via a normal in-module import (no workspace replace directive needed). | covered | Package lives at `packages/go-client/` with no per-package `go.mod`. The root `go.mod` (module `hackathon`) covers it transitively, and the package compiles via `go test ./packages/go-client/...` at this SHA. No `replace` directive needed because there's no second module. apps/cli does not yet import the package (it's the next-feature wiring step), but the import shape is verifiable: any file under apps/cli could write `import goclient "hackathon/packages/go-client"` and the module graph resolves it without further configuration. |

## Findings

### Coverage notes

- **`MaxResponseBytes` symmetry with server-side `BodyCap`.** `client.go:33` caps response reads at 1 MiB to mirror the server's request-body cap. This is defensive — a hostile upstream could otherwise stream arbitrarily large bytes into `io.ReadAll` and OOM the client. Not asserted by AC text but the right kind of paranoia for a client package and worth flagging.
- **`DefaultTimeout = 30s`** for REST requests is REST-only; `Watch`'s lifetime is bound to the caller's `ctx`, not the HTTP timeout. This separation is correct: a long-lived WS subscription would silently die under a 30s read timeout. The doc-comment on `DefaultTimeout` (`client.go:24-27`) explicitly calls this out so future readers don't fold WS into the same constant.
- **`decodeEvent` typed-frame contract.** `ws.go:96-113` parses inbound frames as `{type:..., data:...}` envelopes; when present, populates `Event.Type` + `Event.Message` (for `type:"message"`); when absent, returns the raw bytes in `Event.Raw`. `ws_test.go::TestWatchUnknownFrameSurfacesAsRaw` proves the raw-fallback path works, so the phase-0 raw-rebroadcast contract on `#general` is still consumable through this client without losing data. This is right factoring: the typed shape is a server roadmap (`feature-channels-and-messages` plus future presence frames per `30-feature-ts-api-client-package.md` AC-6); the raw fallback keeps the client useful today while the contract converges.
- **`SetReadLimit(64 KiB)`** on the WS connection (`ws.go:67`) mirrors `wsapi.ReadLimitBytes` — frames larger than that would have been truncated server-side anyway, so accepting them client-side just buffers attacker-controlled bytes. Not in any AC; right defensive default.
- **Ticket flow is internally redeemed by `Watch`.** Callers don't manually mint+redeem; `Watch` calls `WsTicket` internally (`ws.go:47`) and uses the returned ticket on the upgrade. This is a usability win — one method, no two-step dance — and matches the "single-shot redemption" SEC contract because `WsTicket` is invoked once per `Watch`. The TS client makes the same choice (`client.ts:84-92` exposes `watch()` similarly).

### Cross-feature observations

- **AC-4 wiring deferred to `20-feature-cli-full-commands`.** The CLI doesn't yet import this package; `apps/cli/main.go` and `apps/cli/cmd/url.go` are unchanged at this SHA. The spec (and the parent phase doc) treats this as the explicit next step. The "consumable from apps/cli via a normal in-module import" AC is satisfied at the import-graph level (the import resolves) without requiring an actual call site to exist — which is the right reading, because demanding the call site would couple this AC to a different feature's PR.
- **Server contract anchors.** The `EventTypeMessage = "message"` constant (`ws.go:16`) carries a doc-comment pointing at the server-side `WSEventMessage` symbol so a future schema-drift PR can grep both sides simultaneously. Same convention as `MaxResponseBytes` ↔ server `BodyCap`. Worth keeping.
- **Test counts.** 23 tests total: 7 in client_test.go, 7 in auth_test.go, 3 in channels_test.go, 4 in messages_test.go, 3 in ws_test.go. `TestWatchEndToEnd` (ws_test.go:21) is the load-bearing integration test — it stands up an httptest.Server with a stub `wsapi.Handler`, mints a ticket, redeems it, and verifies a posted message arrives on the events channel. Without this test, AC-3's WS half would only be covered structurally.

### Spec-vs-impl notes

- Spec lists `client.go`, `auth.go`, `channels.go`, `messages.go`, `ws.go`, `*_test.go` — all present. No drift.
- The package name is `goclient` (one word, lowercase) per Go convention; the spec implicitly mandates this by saying "imported as `hackathon/packages/go-client`" without specifying a package name. `goclient` is the natural choice.
- `Option` pattern is used (`WithHTTPClient`, `WithToken`); spec mentions `WithBearer` round-tripper in implementation step 5. The implementation uses `WithToken` instead, which is functionally equivalent and slightly cleaner (no transport wrapper). Spec follow-up could update the wording.
- `ListMessagesOptions` (`messages.go:35`) carries `Before` (cursor) and `Limit` — the spec doesn't enumerate the option fields, but these are the right ones given the server's pagination contract (`feature-channels-and-messages` AC-3).
- Two follow-up commits after the original PR (8ed4e82 + a4a5980) tightened response-size caps and ws-upgrade-body-close. These are the kind of post-merge hardening that's appropriate for a client package; they don't change the AC coverage shape.

## Recommendations

1. No new tests added by this run — coverage is comprehensive across all 4 ACs at the unit + integration boundary, including a real ticket-redemption end-to-end test for the WS path.
2. **Cross-feature follow-up:** when `20-feature-cli-full-commands` lands and `apps/cli` actually imports `hackathon/packages/go-client`, re-evaluate this feature's AC-4 to confirm the import-graph claim holds at a real call site (it should, but worth a check at the SHA where the wiring lands).
3. **Spec follow-up (out of test-agent scope):** update implementation step 5's "WithBearer round-tripper" wording to match the shipped `WithToken` option name, so future implementers don't go looking for a missing helper.
