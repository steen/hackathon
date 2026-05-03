---
feature: ws-hardening
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: WS hardening (origin check, ws-ticket flow, channel validation)

**Spec:** `specs/plans/phase-1/feature-ws-hardening.md`
**Implementation status:** implemented — `apps/server/internal/wsapi/handler.go` redeems ws-tickets and applies origin patterns from `wsCfg.OriginPatterns` (parsed from `CHAT_ALLOWED_ORIGINS` at `apps/server/main.go:101-103`); ticket store at `apps/server/internal/auth/tickets.go`; channel-existence wiring at `main.go:104-106` via `wsCfg.ChannelLookup = repository.ChannelExists`. Note: per the spec's "Implementation notes," same-origin enforcement is delegated to `coder/websocket.Accept`'s default behaviour and pre-upgrade ticket failures return HTTP 401.
**E2E test directory:** `tests/e2e/phase-1/ws-hardening/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | WS upgrade enforces a same-origin check; cross-origin upgrades are rejected with a 403. | missing | — |
| AC-2 | WS connections must present a valid one-shot ticket from `POST /api/auth/ws-ticket`; tickets expire after 30 seconds and are single-use. | missing | — |
| AC-3 | After successful ticket redemption, the WS connection is associated with the authenticated user identity. | missing | — |
| AC-4 | WS sends to non-existent channels are rejected with a typed error frame and do not crash the connection. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — same-origin check, cross-origin → 403**
- **What to assert:** Boot the server with a known `CHAT_ALLOWED_ORIGINS` (e.g. `http://localhost:3000`). Dial `/ws` over `coder/websocket.Dial` with an explicit `Origin: http://evil.example.com` header (set via `websocket.DialOptions{HTTPHeader: http.Header{"Origin": ...}}`); assert `Dial` returns an error AND the returned `*http.Response` has `StatusCode == 403`. Then dial with `Origin: http://localhost:3000` → assert success (status 101). Then dial with no Origin header (loopback case during dev) → assert success (the spec says loopback dev path is allowed; verify `coder/websocket`'s default allows missing Origin from same-host clients — the gold-standard hub test confirms an empty-Origin loopback dial succeeds).
- **Layer:** Go (boot binary, raw WS dial with custom Origin).
- **File path:** `tests/e2e/phase-1/ws-hardening/origin_check_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_ALLOWED_ORIGINS=http://localhost:3000`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. `github.com/coder/websocket` for the dial.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `dialWSWithOrigin(t, srv, origin string, ticket string)`.

**AC-2 — ticket required, single-use, 30s TTL**
- **What to assert:** Register + login. `POST /api/auth/ws-ticket` with bearer → 200 envelope `{ok:true, data:{ticket:"<value>"}}`. `dialWS` with `?ticket=<value>` → success (101). Repeat the dial with the *same* `?ticket=<value>` → assert non-101 (likely 401 per the spec's "Implementation notes"). Issue a fresh ticket; sleep 31 seconds; dial → assert non-101 (likely 401, expired). Dial with `?ticket=garbage` → 401. Dial without `?ticket=` → 401 (when `tickets != nil` in `wsCfg`; verify by reading `handler.go` once during test authoring). Negative-overall: dialing with no ticket against a server booted *without* `CHAT_DB_PATH` (so `tickets == nil` and the check is skipped) → 101 — but in the production wiring path, no ticket means rejection.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/ws-hardening/ticket_flow_test.go`.
- **Setup it needs:** same as AC-1; the 30s expiry test is slow — gate the expiry arm with `if testing.Short() { t.Skip("waits 31s for ticket TTL") }`.
- **Helpers it can reuse:** harness; `register`, `login`, `wsTicket`, `dialWS(t, srv, ticket)`.

**AC-3 — redeemed ticket binds user_id to the connection**
- **What to assert:** Register users `alice` and `bob`. Each obtains its own ticket via `/api/auth/ws-ticket`. Both dial `/ws?ticket=<own>` → both succeed (101). The bound user_id must be observable from outside; the `ws-userid-binding-and-channel-existence-check` feature confirms the wiring is now `state.userID = userID`. Two observable proxies:
  - (preferred) Send a message via `POST /api/channels/{id}/messages` from each user; the WS broadcast frames received by the *other* connection should carry `user_id` matching the sender. This proves the server attributes correctly using the bound identity. (This overlaps with `channels-and-messages` AC-5 — that's expected; AC-3 here pins the binding-side claim.)
  - (alternative) `GET /api/presence` (wired at `main.go:153`) which lists hub presence; the response should include both alice and bob's user ids when their WS connections are open. After alice's WS closes, presence should drop alice within a short window.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/ws-hardening/userid_bound_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`, `login`, `wsTicket`, `dialWS`, `getPresence(t, srv, tok)`.

**AC-4 — WS send to non-existent channel rejected with typed error frame, no crash**
- **What to assert:** This AC's literal text expects a typed error *frame* on the established WS, not an upgrade-time 404. The follow-up plan `ws-userid-binding-and-channel-existence-check` notes that at this SHA the channel-existence check happens at *upgrade* time (HTTP 404), not in a per-frame inbound parser, because inbound frames are dropped post-audit-#78. Two observable arms:
  - Upgrade arm (the one that actually exists): dial `/ws?ticket=<valid>&channel=NOT-A-REAL-ULID` (the harness must include the channel param the way `wsapi/handler.go` reads it — verify by reading `handler.go` once during test authoring); assert non-101 status with body matching the standard error envelope. The `#general` legacy channel passes; a freshly created ULID channel passes; a random 26-char string that's not in the channels table fails 404.
  - Frame arm (deferred-but-write-the-test-skipped): once typed inbound frames land, dial with valid ticket and channel `#general`; write a frame `{"type":"send","channel_id":"BAD","body":"hi"}`; expect a frame back `{"type":"error","code":"CHANNEL_NOT_FOUND"}`; assert connection still open by reading the next frame within a short window. Mark with `t.Skip("typed inbound frames not implemented; tracked in ws-userid-binding-and-channel-existence-check")` until the contract is in place.
- **Layer:** Go (boot binary, WS).
- **File path:** `tests/e2e/phase-1/ws-hardening/channel_validation_test.go`.
- **Setup it needs:** same as AC-1 + a second helper test that creates a channel via `POST /api/channels` to give a known-good ULID for the positive case.
- **Helpers it can reuse:** harness; `dialWS`, `dialWSChannel(t, srv, ticket, channel)`, `createChannel`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. AC-1 needs a `dialWSWithOrigin` variant that sets `websocket.DialOptions.HTTPHeader["Origin"]` — write that once, share across tests.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/ws-hardening/harness_test.go` with copied helpers + `register`, `login`, `wsTicket`, `dialWS`, `dialWSChannel`, `dialWSWithOrigin`, `createChannel`, `getPresence`.
- Add `origin_check_test.go` (AC-1), `ticket_flow_test.go` (AC-2 — gate the 31s arm on `!testing.Short()`), `userid_bound_test.go` (AC-3), `channel_validation_test.go` (AC-4 — frame arm `t.Skip` until typed frames land).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
