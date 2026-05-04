---
feature: body-and-ws-caps
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: Body and WS read/send caps

**Spec:** `specs/plans/phase-1/feature-body-and-ws-caps.md`
**Implementation status:** implemented — REST body cap lives in `apps/server/internal/http/limits.go` (wired as `BodyCap` in `main.go` line 174). WS read limit + per-conn rate limit live in `apps/server/internal/wsapi/handler.go` and `apps/server/internal/wsapi/ratelimit.go`. Tests in `apps/server/internal/wsapi/limits_test.go` cover the WS side.
**E2E test directory:** `tests/e2e/phase-1/body-and-ws-caps/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | WebSocket reads are capped at 64 KiB per frame; oversized frames close the connection with a policy-violation code. | missing | — |
| AC-2 | Each WS connection has a per-conn send rate limit (e.g., N messages/sec with burst); excess sends are dropped or trigger close. | missing | — |
| AC-3 | WS message bodies (chat-message payloads) are capped at 4 KiB. | missing | — |
| AC-4 | REST request bodies are capped at 16 KiB; oversized bodies return 413. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — WS frame >64 KiB closes connection (1009)**
- **What to assert:** Boot the server. Dial `ws://127.0.0.1:<port>/ws` with `coder/websocket.Dial`. The WS read limit (`wsapi.ReadLimitBytes = 64*1024`) and the WS body cap (`wsapi.MessageBodyLimit = 4*1024`) both close with code 1009 (StatusMessageTooBig), so the at-limit and over-limit cases are distinguished by close *reason*, not code — any frame >4 KiB trips AC-3's body cap on the inbound path, so "stays open" is impossible at 64 KiB and the original sketch was wrong on that point. Write a single text frame of exactly `64*1024` bytes → the read-limit does not fire, the frame is delivered to `readLoop`, and the body-cap branch closes with code 1009 + reason `"message body exceeds 4 KiB limit"`. Dial fresh; write a single text frame of `64*1024 + 2` bytes → assert `Read` returns an error wrapped with `*websocket.CloseError` whose `Code == 1009` and whose `Reason` does NOT mention "4 KiB" (the read-limit fires inside `coder/websocket` before `readLoop` sees the bytes; PRD §11 SEC-6, RFC 6455 StatusMessageTooBig). The `+2` (not `+1`) is deliberate: `coder/websocket@v1.8.14`'s `SetReadLimit(n)` stores `n+1` internally to reserve room for the trailing FIN frame, so a `64*1024 + 1` frame is delivered in full and only the body-cap branch fires; `64*1024 + 2` is the smallest payload that trips the read-limit branch. Use `websocket.CloseStatus(err)` to extract the code portably and read `(*websocket.CloseError).Reason` for the disambiguating text. Asserting on reason text is fragile but is the only externally-visible signal that the read-limit cap is positioned at >= 64 KiB rather than lower (see PR #299 for the landed pattern).
- **Layer:** Go (boot binary, raw WS).
- **File path:** `tests/e2e/phase-1/body-and-ws-caps/ws_frame_size_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`. CHAT_DB_PATH only required if WS is gated behind ticket auth in the relevant config — gold-standard test dials without a ticket, so omit DB to keep `tickets == nil` mode.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard.

**AC-2 — per-conn send rate limit drops excess or closes**
- **What to assert:** Dial `/ws`. Write N+1 small text frames in a tight loop where N is the configured burst (verify by reading `wsapi/ratelimit.go` once during test authoring; if not exposed, infer by escalating loop count until behaviour changes). Behaviours allowed by the AC: (a) excess frames are silently dropped — assert via a side-channel (e.g. only N broadcasts reach a second observer client subscribed to the same channel), or (b) connection is closed — assert via `Read` returning a CloseError with code 1008 (policy violation) or 1009. Since inbound frames are dropped post-audit-#78 (see `tests/server-ws-hub/hub_test.go` AC-3 comment), the observable signal must come from the rate-limiter's own action — likely a close or an error frame. The test should boot the server, blast 1000 frames, and assert that the connection is no longer writable within 1 second (`Write` returns error). If the rate limiter only drops silently, fall back to checking `auth_events` or server logs for a rate-limit log line.
- **Layer:** Go (boot binary, raw WS).
- **File path:** `tests/e2e/phase-1/body-and-ws-caps/ws_send_rate_limit_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1.

**AC-3 — WS message body >4 KiB rejected**
- **What to assert:** This depends on the WS message envelope having a `body` field that the server inspects. At this SHA, inbound WS frames are dropped (`tests/server-ws-hub/hub_test.go` AC-3 comment confirms post-audit-#78), so a 4 KiB body cap on the inbound path is not externally observable through WS at all. The realistic E2E proxy is the REST send path: register + login + create channel; `POST /api/channels/{id}/messages` with `body` of exactly 4096 chars → 200 (or 201) envelope; with body of 4097 chars → 4xx envelope. Open SQLite read-only and assert no row was inserted for the rejected attempt. Note in the test docstring that this AC's literal target is the WS send path, but inbound WS frames are intentionally dropped, so the assertion sits on the REST mirror of the same body-size invariant.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/body-and-ws-caps/ws_message_body_cap_test.go`.
- **Setup it needs:** same as AC-1 + `CHAT_DB_PATH=<tmpdir>/chatd.sqlite` so the channels/messages handlers are wired (see `main.go` line 109 conditional).
- **Helpers it can reuse:** harness; `register`, `login`, `createChannel(t,srv,tok,name)`, `sendMessage(t,srv,tok,chID,body) (status int, body []byte)`.

**AC-4 — REST body >16 KiB returns 413**
- **What to assert:** Boot server. `POST /api/auth/register` with a JSON body whose total wire length is exactly 16384 bytes → status is *not* 413 (likely 400/422 for invalid contents, or 200 if happens to parse — the point is the cap doesn't fire). With body of 16385 bytes → status 413. Body must be `{"ok":false,"error":{"code":"<some code>","message":"..."}}` envelope (verify the exact code by reading `limits.go` once during test authoring; PRD §10 envelope shape is mandatory). Test the cap on `/api/auth/login` and `/api/auth/register` (both wired). Confirm headers from outer middleware (`Content-Security-Policy`, `X-Content-Type-Options`) are still present on the 413 response — tests the outer-most ordering of `SecurityHeaders`.
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/body-and-ws-caps/rest_body_cap_test.go`.
- **Setup it needs:** same as AC-3.
- **Helpers it can reuse:** harness; `requireEnvelope(t,body,...)`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. AC-1's WS test should re-read the `CloseStatus` helper pattern from `coder/websocket`'s docs to get a portable `*websocket.CloseError` extractor.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/body-and-ws-caps/harness_test.go` with copied helpers + `register`, `login`, `createChannel`, `sendMessage`, `requireEnvelope`, `dialWS`, `closeStatus(err) int`.
- Add `ws_frame_size_test.go` (AC-1), `ws_send_rate_limit_test.go` (AC-2), `ws_message_body_cap_test.go` (AC-3, REST proxy), `rest_body_cap_test.go` (AC-4).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
