---
feature: body-and-ws-caps
phase: phase-1
analyzed_at: 2026-05-03T15:02:47Z
analyzed_commit: e689e8f4f11e411a38a6c99fa2ecfc35847e1307
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Body and WS read/send caps

**Spec:** `specs/plans/phase-1/feature-body-and-ws-caps.md`
**Implementation status:** implemented — all four caps are in place and wired:
- WS read limit (`SetReadLimit(64 KiB)`) is set on every accepted connection in `wsapi.Handler`.
- WS body cap (4 KiB) is checked in `readLoop` after each `Read`.
- Per-conn token bucket (burst 30, 10/s steady-state) is allocated per-connection in `Handler` and consulted in `readLoop`.
- REST body cap (16 KiB) is enforced via `httpx.BodyCap` middleware, wrapped around the mux in `apps/server/main.go:72` so every REST route inherits it.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | WebSocket reads are capped at 64 KiB per frame; oversized frames close the connection with a policy-violation code. | covered | `apps/server/internal/wsapi/limits_test.go::TestWSRejectsFrameOver64KiBClose1009` (writes `ReadLimitBytes+1` bytes, asserts close code is `StatusMessageTooBig` (1009), and asserts the library constant equals 1009 — guards against a future library version that renumbers the close code). |
| AC-2 | Each WS connection has a per-conn send rate limit; excess sends are dropped or trigger close. | covered | `apps/server/internal/wsapi/limits_test.go::TestWSSendRateLimitClosesPolicyViolation` (floods burst+20 frames, polls for the close, asserts code is `StatusPolicyViolation` (1008) + library-constant guard). |
| AC-3 | WS message bodies (chat-message payloads) are capped at 4 KiB. | covered | `apps/server/internal/wsapi/limits_test.go::TestWSRejectsMessageBodyOver4KiB` (writes `MessageBodyLimit+1` between 4 KiB and 64 KiB so it passes the read limit but trips the body cap; asserts close is `StatusMessageTooBig`). |
| AC-4 | REST request bodies are capped at 16 KiB; oversized bodies return 413. | covered | `apps/server/internal/httpx/limits_test.go::TestRESTRejectsBodyOver16KiBWith413` (oversize POST → 413 + `body_too_large` envelope + downstream handler not invoked) + `TestRESTAllowsBodyAtLimit` (boundary; at-limit body passes through with full read) + `TestWriteMessageTooLargeEnvelope` (REST-side message-too-large envelope shape). |

## Findings

### Coverage notes

- **Behavioral, not internal.** The token bucket has no isolated unit test (no `tokenBucket_test.go`), but the AC is "excess sends close the connection" — exactly what `TestWSSendRateLimitClosesPolicyViolation` proves end-to-end through the public `wsapi.Handler`. A separate unit test on `tokenBucket.allow()` would be a refactor-coupling test (any implementation that closes after burst is fine; the bucket happens to be how it's done today). Skipped to avoid that coupling.
- **Library-constant guards.** Both WS tests include an explicit assertion that `int(websocket.StatusMessageTooBig) == 1009` and `StatusPolicyViolation == 1008`. Catches a hypothetical library upgrade that re-defines the constants without renumbering — the AC names the protocol-level code, not the Go symbol.
- **Two distinct close paths.** The 64 KiB read limit closes inside the websocket library before `readLoop` ever sees the data; the 4 KiB body cap closes from `readLoop` after a successful `Read`. The two tests pick frame sizes that exercise each path independently (>64 KiB for AC-1, between 4 KiB and 64 KiB for AC-3). Important because a single combined test could mask the read-limit not being set if the body-cap branch caught it.
- **Wiring is real.** `apps/server/main.go:72` wraps the mux with `httpx.BodyCap`. This is the same wiring shape `phase-1/file-perms-and-headers` AC-2/AC-3 are still missing (`SecurityHeaders` not applied). Worth flagging because the pattern now exists in the codebase — the next implementer can compose `httpx.BodyCap(SecurityHeaders(mux))` cleanly.

### Spec-vs-impl notes

- Spec lists `apps/server/internal/ws/handler.go` and `apps/server/internal/http/middleware_bodycap.go` as expected file paths. Impl uses `apps/server/internal/wsapi/handler.go` (existing package, modified) and `apps/server/internal/httpx/limits.go` (new package). The package name `httpx` deliberately coexists with the older `http` package — the latter still owns `SecurityHeaders` and the access log; the new `httpx` is where REST body/limit helpers live. There's a real risk these two packages confuse a future implementer; spec follow-up could either consolidate them or document the boundary.
- The new `httpx.Envelope` / `httpx.WriteError` / `httpx.WriteBodyTooLarge` mirror the older `http.Envelope` / `http.WriteError` from `feature-logging-and-error-envelope`. They are NOT shared — `httpx` defines its own. Functionally identical today; the next refactor should pick one home for the canonical envelope helpers and have the other re-export.

## Recommendations

1. No new tests added by this run — coverage is appropriate at the public-API layer for all 4 ACs.
2. Cross-feature observation worth surfacing: `httpx.BodyCap(mux)` wiring at `main.go:72` is the same pattern `feature-file-perms-and-headers` needs for `SecurityHeaders`. PR #37 already flagged that gap; it should now be a one-line follow-up to compose `httpx.BodyCap(SecurityHeaders(mux))` (order matters — security headers should be the outermost so they apply to the 413 envelope too).
3. Spec / refactor follow-up (out of test-agent scope): two near-duplicate envelope packages (`http` vs `httpx`) is a footgun. Pick one canonical home and re-export from the other.
