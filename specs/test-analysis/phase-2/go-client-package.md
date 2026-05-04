---
feature: go-client-package
phase: phase-2
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: `packages/go-client` (HTTP + WS client)

**Spec:** `specs/plans/phase-2/10-feature-go-client-package.md`
**Implementation status:** implemented — `packages/go-client/` ships `client.go`, `auth.go`, `channels.go`, `messages.go`, `ws.go` (and matching `*_test.go`) under `package goclient`, importable as `hackathon/packages/go-client` from the single root `go.mod`.
**E2E test directory:** `tests/e2e/phase-2/go-client-package/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | A reusable Go package at `packages/go-client` (part of the single-root `hackathon` module, imported as `hackathon/packages/go-client`) exposes typed methods for: `Login`, `Register`, `Me`, `Logout`, `ListChannels`, `CreateChannel`, `ListMessages`, `PostMessage`, `WsTicket`, and `Watch` (returns a stream of inbound events). | missing | — |
| AC-2 | The client handles base URL, auth token storage (in memory), and JSON/error-envelope decoding (envelope shape is `{ok, data, error: {code, message}}` per `apps/server/internal/http/errors.go`). | missing | — |
| AC-3 | HTTP requests authenticate with `Authorization: Bearer <jwt>`; WebSocket connections use the one-shot ticket flow — call `WsTicket` to mint a ticket, then redeem it on upgrade as `?ticket=<hex>` (see `apps/server/internal/wsapi/handler.go` and `feature-ws-hardening.md`). Bearer tokens are not sent on the WS upgrade. | missing | — |
| AC-4 | The client is consumable from `apps/cli` via a normal in-module import (no workspace replace directive needed). | missing | — |

## Findings

### Missing E2E tests

All four ACs need black-box tests that drive `goclient.New(srv.URL)` against a real `apps/server` binary booted on a random loopback port with random secrets. The in-package `*_test.go` files use `httptest.Server` with stub handlers, which is fine for unit coverage but does not exercise the real server's auth handlers, ticket store, hub, or middleware chain — so they do not satisfy E2E coverage.

- **AC-1 sketch — `tests/e2e/phase-2/go-client-package/surface_test.go`.** Boot `apps/server` via the `startServer(t)` helper from `tests/server-ws-hub/hub_test.go` (env: `CHAT_SERVER_PORT`, `CHAT_JWT_SECRET`, `CHAT_INVITE_CODE`, `CHAT_DB_PATH=<t.TempDir()>/chat.db`). Construct `c := goclient.New(srv.url)` and walk the surface end-to-end:
  1. `Register(ctx, "alice", "password-32-bytes-min", inviteCode)` → expect `*RegisterResponse` with non-empty `Token`.
  2. `c.SetToken(resp.Token)` then `Me(ctx)` → expect `User{Username:"alice"}`.
  3. `CreateChannel(ctx, "#test")` → expect channel object with non-empty `ID`.
  4. `ListChannels(ctx)` → expect the just-created channel present.
  5. `PostMessage(ctx, channelID, "hello")` → expect non-empty message ID.
  6. `ListMessages(ctx, channelID, ListMessagesOptions{Limit:50})` → expect the posted message.
  7. `Logout(ctx)` → expect nil error; subsequent `Me(ctx)` returns 401-mapped `*APIError`.
  Each call asserts both the typed return value AND that the call did not panic / return an unexpected error type. AC-1 is satisfied when all 10 methods on the public surface have one happy-path round-trip in a single test against the real server.

- **AC-2 sketch — `tests/e2e/phase-2/go-client-package/envelope_test.go`.** Same server boot. Three sub-tests:
  1. **Base URL normalization:** `goclient.New(srv.url + "/")` and `goclient.New(srv.url)` both succeed at `Me()` after a valid login. The trailing slash must not break path concatenation.
  2. **In-memory token storage:** instantiate two `*Client` values pointed at the same server; set distinct tokens via `SetToken`; concurrent `Me()` calls return distinct usernames. Asserts no shared global state.
  3. **Error-envelope decode:** call `Login(ctx, "nope", "wrongpassword")` against the real server → expect `*goclient.APIError` with `Status==401`, `Code=="..."` (whatever the server uses for bad creds — check the auth handler), and a non-empty `Message`. Then call `PostMessage` with a 2 MiB payload → expect a 413 mapped to an `*APIError` with the documented code.

- **AC-3 sketch — `tests/e2e/phase-2/go-client-package/auth_transport_test.go`.** Boot the server. Three sub-tests:
  1. **Bearer on REST:** wrap `srv.url` with a `httptest`-style intercepting transport (or use a sniffing `http.RoundTripper` injected via `WithHTTPClient`) and call `Me()` after `SetToken("tok-...")`. Assert the captured request has header `Authorization: Bearer tok-...`. Then unset the token and call `ListChannels` (which the server may permit unauth or reject — the assertion is about the header, not the response): assert no `Authorization` header on the wire.
  2. **Ticket round-trip on WS:** register + login a real user → call `WsTicket(ctx)` → expect a non-empty hex ticket string. Then `Watch(ctx, channelID)` should mint+redeem internally; capture the upgrade URL via a custom dialer (or hit `/debug/subs?channel=<id>` like `tests/server-ws-hub/hub_test.go` does to verify the subscription registered). Assert the subscriber count went to 1.
  3. **No bearer on WS upgrade:** intercept the WS dial via a custom `*http.Client` in `WithHTTPClient` so the harness sees the raw upgrade request headers. Assert no `Authorization` header is present even when `c.SetToken` was called before `Watch`.

- **AC-4 sketch — `tests/e2e/phase-2/go-client-package/cli_import_test.go`.** This AC is import-graph in nature; the cleanest E2E proof is `exec.Command("go", "build", "-o", t.TempDir()+"/probe", "./tests/e2e/phase-2/go-client-package/probe")` where `probe/main.go` is a tiny program inside the test directory that `import "hackathon/packages/go-client"` and calls `goclient.New("http://x")`. Build success at the repo root proves the no-replace-directive claim. (Alternative: the same test boots the real server, runs the actual `chatd` binary against it via `os/exec`, and asserts a `chatd channels` round-trip succeeds; this also exercises the import path because `apps/cli` imports `goclient` — but that overlaps with the `cli-full-commands` E2E suite. Pick the build-probe shape to keep this feature's E2E focused.)

### Helpers and harness notes

- Lift `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort`, and `repoRoot(t)` from `tests/server-ws-hub/hub_test.go` into a shared `tests/e2e/internal/serverharness/` package so each phase-2 E2E feature directory imports the same boot helper. Avoid copy-paste — the boot env is the contract surface (env var names, secret sizes) and drift between copies is what guarantees future flakes.
- The harness must add `CHAT_DB_PATH=<t.TempDir()>/chat.db` (the `tests/server-ws-hub/hub_test.go` boot omits this because phase-0 didn't need a DB; phase-2 auth handlers are gated on `repository != nil` per `apps/server/main.go:104-154`, so without a DB path the auth routes don't even register).
- Use `t.Parallel()` on every E2E test — `freePort(t)` already returns distinct ports per call, and each test gets its own DB tempfile.
- For the AC-3 transport-sniffing trick, expose a custom `http.RoundTripper` via `goclient.WithHTTPClient(&http.Client{Transport: sniffer})`. The sniffer records request headers per call and forwards via `http.DefaultTransport`. Don't try to monkey-patch `goclient` internals.

## Recommendations for /test-implement

1. **First:** create `tests/e2e/internal/serverharness/serverharness.go` (or similar) containing `Start(t) *Server`, `RandomSecret`, `FreePort` — it's the dependency every phase-2 E2E suite will need. One PR.
2. **Then:** add `tests/e2e/phase-2/go-client-package/surface_test.go` (AC-1) and `envelope_test.go` (AC-2) — they share boot and are the foundation for the other ACs. One PR.
3. **Then:** add `auth_transport_test.go` (AC-3) — depends on the same harness; the WS-no-bearer assertion needs the sniffer plumbing first.
4. **Last:** `cli_import_test.go` (AC-4) — smallest, can wait; if `apps/cli` already imports `goclient` at the SHA when this lands (it does, per `apps/cli/cmd/login.go` etc.), this is a single `go build` shellout.
