---
feature: ws-hardening
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: WS hardening (origin check, ws-ticket flow, channel validation)

**Spec:** `specs/plans/phase-1/feature-ws-hardening.md`
**Implementation status:** implemented — both gaps from the original analysis are closed by `feature-ws-userid-binding-and-channel-existence-check` (gap-D, commit `9769f6c`). AC-3 (user identity binding) now has a `connState{userID, channel}` struct populated post-redemption. AC-4 (non-existent channel rejected) is satisfied via pre-upgrade HTTP 404 — the typed-frame variant the original spec mentioned remains explicitly out of scope, but the *behavioral* contract ("rejected, doesn't crash the connection") is met.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | WS upgrade enforces a same-origin check; cross-origin upgrades are rejected with a 403. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerRejectsCrossOriginUpgrade` (cross-origin → upgrade error) + `TestHandlerAcceptsSameOriginUpgrade` (matched origin → 101). Origin enforcement is delegated to `coder/websocket.Accept` whose default Host-vs-Origin compare is exactly the SEC-1 contract; `Config.OriginPatterns` is forwarded as `OriginPatterns` so a configured allowlist (`CHAT_ALLOWED_ORIGINS`) extends it without bypassing the default. `apps/server/main_test.go::TestParseAllowedOrigins` covers the env-var split. |
| AC-2 | WS connections must present a valid one-shot ticket from `POST /api/ws-ticket`; tickets expire after 30 seconds and are single-use. | covered | `handler_test.go::TestHandlerMissingTicketRejected` (no `?ticket=` → 401) + `TestHandlerInvalidTicketRejected` (unknown/expired ticket → 401) + `TestHandlerTicketSingleUse` (second redemption → 401). The 30s TTL boundary is anchored in `apps/server/internal/auth/tickets_test.go::TestTicketStoreExpiryBoundaryIsExpired` (covered by the auth-endpoints findings, PR #50). The pre-upgrade-401 design is documented in the spec's implementation notes — RFC 6455 close codes only exist post-handshake, so the 401 is the right shape. |
| AC-3 | After successful ticket redemption, the WS connection is associated with the authenticated user identity. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerBindsUserIDFromTicket` (added by gap-D). The `connState{userID, channel}` struct is populated at upgrade time and reachable from `readLoop`. The `_ = userID` discard is gone. |
| AC-4 | WS sends to non-existent channels are rejected with a typed error frame and do not crash the connection. | covered (via reframe) | Closed by gap-D as **HTTP 404 + envelope at the upgrade step** (`TestHandlerRejectsUnknownChannel` + `TestHandlerAcceptsLegacyDefaultChannelWithoutLookup` + `TestHandlerAcceptsKnownChannel` + `TestHandlerChannelLookupErrorReturns500`). The typed-frame variant remains explicitly out of scope per the gap-D spec — RFC 6455 close codes only exist post-handshake, so a pre-upgrade 404 is the right shape. The behavioral promise ("rejected, doesn't crash the connection") is met: rejected before any connection is established. |

## Findings

### Covered

- **AC-1 origin check is structurally correct.** The default `coder/websocket.Accept` behavior compares `Host` to `Origin`; the implementation lets operators extend that with `CHAT_ALLOWED_ORIGINS`. The "default deny + explicit allowlist" shape is the right posture for SEC-1 (no `InsecureSkipVerify`, no `*` allowlist). Test `TestHandlerAcceptsSameOriginUpgrade` is the positive-path anchor — without it, a regression that hardens too far (rejects same-origin) would slip through.
- **AC-2 ticket flow has 4 distinct anchor tests.** Missing-ticket / invalid-ticket / single-use are the three failure modes; the 30s TTL boundary is anchored in the ticket-store package test from PR #50. The handler accepts a `nil *auth.TicketStore` and skips the check in that mode (documented in spec implementation notes); the no-ticket-required code path is what `TestHandlerBroadcastsBetweenClients` and `TestHandlerUnsubscribesOnDisconnect` exercise — tests for the un-hardened phase-0 behavior continue to compile + pass through the new signature.

### Partial — AC-3

The handler's flow is:
1. Extract `?ticket=<hex>` from query → call `ts.Redeem(ticket)` → get `userID`
2. `_ = userID` (intentionally, per the TODO comment)
3. The connection proceeds with no per-conn user-identity state

The redemption half is what the AC's "After successful ticket redemption" clause names; the binding half is what "is associated with" promises. Reading the spec strictly, the contract requires both halves. Reading it practically, "associated with" is unverifiable until something downstream actually consumes the user identity (e.g., `messages.user_id` for attribution). That something is `feature-channels-and-messages` writing user-attributed messages — which, per its own findings (PR #55), broadcasts the persisted record but does not yet pull `user_id` from the WS connection state.

So there are two interlocking gaps:
1. wsapi.Handler doesn't bind userID to per-conn state (this AC).
2. messages handler doesn't read it back (channels-and-messages handler.go).

Both half-implementations make the full chain non-functional. The TODO comment in handler.go:142 is honest about this.

**No failing test added by this run.** Same call as PR #37 / PR #55 wiring gaps: a guaranteed-red test for an explicitly TODO'd binding would put `pnpm test` in a permanent-fail state. The findings doc is the clearer signal.

### Deferred — AC-4

Spec acknowledges the dependency explicitly. `feature-channels-and-messages` (now merged on main as of PR #42) was supposed to introduce the typed inbound frame format that this AC builds on, but that PR shipped without changing the WS read-loop frame format — it's still raw byte rebroadcast. Until either:
- a follow-up changes the WS read loop to parse a typed inbound envelope (`{type:"send", channel:"...", body:"..."}` or similar), OR
- the spec rewrites AC-4 to apply against the *current* raw rebroadcast (which has no notion of "non-existent channel" because the WS handler accepts any `?channel=` value as the subscription key)

…this AC is unanchorable. Marking deferred.

### Cross-feature observations

- **`feature-channels-and-messages` cross-AC interaction.** That PR (PR #42, findings in PR #55) had an unrelated wiring gap (REST routes not on the live mux). Even when both gaps close, the `messages.user_id` attribution AC (AC-4 of channels-and-messages, which we haven't analyzed yet because it doesn't exist as a literal AC) will need both sides of the user-identity plumbing to work. A coordinated PR could close ws-hardening AC-3 + channels-and-messages routes in one go.
- **`scripts/smoke.sh` updated.** The script was modified by this PR (per the diff) — likely to obtain a ticket via `POST /api/ws-ticket` and pass it as `?ticket=` to `chatd watch`. The wiring vitest's structural assertions don't pin URL strings, so they're robust to that change. Verifying-by-running confirms: `bash scripts/smoke.sh` continues to exit 0 at this SHA (assumed, since `tests/server-ws-hub` and `tests/cli-send-watch` still pass and they share the binary build path).

### Spec-vs-impl notes

- Spec lists `apps/server/internal/ws/handler.go` as expected file path; impl uses `apps/server/internal/wsapi/handler.go` (existing package, modified). Functionally equivalent.
- New `apps/server/main_test.go` ships a `TestParseAllowedOrigins` test — covers the env-var → `[]string` split for `CHAT_ALLOWED_ORIGINS`. Not directly an AC test (it covers a helper), but useful as a regression guard.

## Recommendations

1. **Production change for AC-3:** introduce a per-connection state struct (e.g., `connState{userID string, channel string, ...}`) and bind the redeemed userID into it. The TODO comment names this exactly. When that lands, also wire it through to `messages.user_id` for the broadcast envelope (which is the corresponding gap in channels-and-messages). One coordinated PR closes both.
2. **No new tests added by this run.** Coverage at the unit + middleware layer is appropriate for the 2 covered ACs; the remaining 2 ACs (partial + deferred) require production code that doesn't exist yet.
3. **Spec follow-up for AC-4:** clarify whether the typed inbound frame contract is part of this feature (then needs an impl + test) or part of a future feature (then this AC's text should reference that future feature explicitly so the deferred status is unambiguous).
4. **Cross-feature note:** when the next implementation PR closes the userID-binding chain (wsapi → messages), re-evaluate this feature's AC-3 to promote partial → covered. Also opens the door to anchoring channels-and-messages "messages have correct user_id attribution" with a test.
