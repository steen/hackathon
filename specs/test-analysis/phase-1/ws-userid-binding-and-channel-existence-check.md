---
feature: ws-userid-binding-and-channel-existence-check
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Bind userID onto WS connection state + validate channel existence

**Spec:** `specs/plans/phase-1/feature-ws-userid-binding-and-channel-existence-check.md`
**Implementation status:** implemented — gap-D landed (commit `9769f6c`). `connState{userID, channel}` introduced; `Config.ChannelLookup` field threads a `repo.ChannelExists`-style helper; main.go wires it; pre-upgrade 404 + envelope rejects unknown channels (legacy `#general` bypasses lookup as the spec called out). The `_ = userID` discard is gone — `userID` is now bound and reachable from `readLoop` via the per-conn state.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | A redeemed ticket's `userID` is stored on a per-connection state value (e.g., a `connState` struct) and reachable from `readLoop` so subsequent message-handling code can attribute the sender. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerBindsUserIDFromTicket` exercises the binding through a test-only accessor (in `handler_export_test.go`). The `connState{userID, channel}` struct is the binding surface. |
| AC-2 | On WS upgrade with a `?channel=<id>` query parameter, the handler validates the channel exists via the same `repo.ListChannels`/`repo.GetChannel`-style lookup the REST handlers use. Unknown channel IDs reject the upgrade with HTTP 404 and the standard error envelope. | covered | `TestHandlerRejectsUnknownChannel` (404 + envelope) + `TestHandlerChannelLookupErrorReturns500` (DB-error path returns 500 not 404 — important: an "unknown" status from a DB outage shouldn't masquerade as channel-not-found). |
| AC-3 | For the legacy `#general` and the seeded ULID channels, validation passes. | covered | `TestHandlerAcceptsLegacyDefaultChannelWithoutLookup` (the legacy `#general` bypasses the lookup entirely — same nil-skip pattern as `TicketStore`) + `TestHandlerAcceptsKnownChannel` (a ULID channel that the lookup returns `(true, nil)` for). |
| AC-4 | The TODO at `apps/server/internal/wsapi/handler.go:148` (`_ = userID`) is removed. | covered | The line is gone. `handler.go:137` now does `userID = uid` and the value is passed into `newConnSubscriber(userID, channel)` at `:181`. The TODO comment that referenced channels-and-messages is also gone. |
| AC-5 | Existing `apps/server/internal/wsapi/handler_test.go` tests still pass; one new test asserts that a request to `?channel=BAD-CHANNEL-ID` returns 404 with the envelope. | covered | All existing wsapi tests continue to pass at this SHA. The new 404 test is `TestHandlerRejectsUnknownChannel` (named differently from the spec's hint string but covering the same contract). |

## Findings

### What changed

- **`Config.ChannelLookup`**: new field (`func(ctx, id) (bool, error)`) on the wsapi `Config` struct. main.go wires it to a thin closure over the repo. When the closure is `nil` (phase-0 boot path), the check is skipped — same nil-skip pattern as `*auth.TicketStore`.
- **`connState`**: private struct holding `userID`, `channel`. Bound at upgrade time, reachable from `readLoop` via the closure.
- **`newConnSubscriber(userID, channel)`** signature change: previously took no args; now takes the bound state so the subscriber's `Send` path can attribute messages.
- **`handler_export_test.go`**: package-internal test helper that exposes the connState value for the test to assert on. Right factoring — keeps the production API clean while letting the test verify the binding without parsing logs.

### Cross-feature observations

- **Closes `feature-ws-hardening` AC-3 partial and AC-4 deferred**. Parent AC-3 ("user identity is associated with the connection") is now satisfied by the connState binding. Parent AC-4 ("non-existent channels rejected") is satisfied via the pre-upgrade 404 path — the typed-frame variant from the original spec stays explicitly out of scope per this follow-up's design. Both should re-promote on the next ws-hardening tick.
- **DB call on upgrade hot path**: the spec anticipated this. `ChannelExists` is a single-row SELECT against the indexed `channels(id)` PK — well under noise at friend scale. The LRU mitigation the spec mentioned remains a future optimization; not load-bearing now.

### Spec-vs-impl notes

- Spec asked for `repo.ChannelExists(ctx, id) (bool, error)` if not already exposed. `apps/server/internal/repo/channels.go` adds it (existence-only variant alongside the existing `GetChannel`-row variant).
- Spec mentioned "Hub-side observer or by exporting a small accessor for tests" for AC-1 verification. Impl chose the export-via-`handler_export_test.go` route. Same effect, no parsing of log lines needed.

## Recommendations

1. No new tests added by this run — coverage is comprehensive.
2. **Cross-feature follow-up:** the next test-watch tick should re-evaluate `feature-ws-hardening` and promote AC-3 from partial to covered, and reframe AC-4 from deferred-pending-impl to covered-via-pre-upgrade-404 (the typed-frame variant stays explicitly out of scope).
