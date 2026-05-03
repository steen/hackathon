---
feature: ws-userid-binding-and-channel-existence-check
phase: phase-1
analyzed_at: 2026-05-03T16:34:51Z
analyzed_commit: 39ce98d17bb0263b7b8988caf323c376aab02a09
implementation_status: stub
total_acs: 5
covered: 0
partial: 0
missing: 0
deferred: 5
---

# Test analysis: Bind userID onto WS connection state + validate channel existence

**Spec:** `specs/plans/phase-1/feature-ws-userid-binding-and-channel-existence-check.md`
**Implementation status:** stub — spec landed (status: planned), no code change. Confirmed at this SHA: `apps/server/internal/wsapi/handler.go:145` is still `_ = userID` with the TODO comment; there is no `connState` struct, no `Config.ChannelLookup` field, no `repo.ChannelExists` helper, no main.go wiring of channel validation.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | A redeemed ticket's `userID` is stored on a per-connection state value (e.g., a `connState` struct) and reachable from `readLoop` so subsequent message-handling code can attribute the sender. | deferred | impl is stub — no `connState` type exists. |
| AC-2 | On WS upgrade with a `?channel=<id>` query parameter, the handler validates the channel exists via the same `repo.ListChannels`/`repo.GetChannel`-style lookup the REST handlers use. Unknown channel IDs reject the upgrade with HTTP 404 and the standard error envelope. | deferred | impl is stub — handler still subscribes to whatever string is in `?channel=` (capped at 64 chars). No DB lookup on the upgrade path. |
| AC-3 | For the legacy `#general` and the seeded ULID channels, validation passes. | deferred | impl is stub — same reason as AC-2. The legacy `#general` exception is the same conditional pattern the handler already uses for `*auth.TicketStore` (skip when nil), so the implementer has prior art. |
| AC-4 | The TODO at `apps/server/internal/wsapi/handler.go:148` (`_ = userID`) is removed. | deferred | impl is stub — confirmed `_ = userID` still present at line 145 (1-line offset from the spec's 148 reference, doesn't matter for status). |
| AC-5 | Existing `apps/server/internal/wsapi/handler_test.go` tests still pass; one new test asserts that a request to `?channel=BAD-CHANNEL-ID` returns 404 with the envelope. | deferred | impl is stub — the existing tests do still pass (verified at this SHA), but the load-bearing half of this AC is the new 404 test, which can't be written without the validation code path. |

## Findings

### Why "stub" rather than "missing"

This is the third planned-only follow-up spec the agent has tracked in this phase (along with `feature-security-headers-and-sqlite-ensure-wiring` PR #47 and `feature-access-log-fields-and-wiring` PR #52 / merged). The maintainer's pattern is consistent: when `feature-X` ships with explicit TODO comments + spec implementation notes acknowledging deferred ACs, a follow-up planned spec is authored to close the loop without re-opening the parent. That keeps the audit clear: parent feature gets credit for what it shipped, follow-up tracks the gap.

This spec exists to close `feature-ws-hardening` AC-3 (partial) and AC-4 (deferred) as flagged in PR #58. The merged code never had any chance of meeting those ACs because:
1. AC-3's "user identity is associated with the connection" requires a per-conn state struct that doesn't exist.
2. AC-4's "channel-not-found typed error frame" requires either (a) a typed inbound frame contract that `feature-channels-and-messages` was supposed to introduce but didn't, or (b) the relaxed pre-upgrade-404 reading this new spec adopts.

The new spec adopts reading (b) for AC-2 and explicitly defers the typed-frame variant to a future spec ("Out of scope: the PRD §10 frame-based WS protocol"). Pragmatic call.

### Cross-feature interactions

- **Closes `feature-ws-hardening` AC-3 partial** when implemented. The parent's findings (PR #58, now merged) flagged the half-done state at handler.go:145; this spec's AC-1 + AC-4 close it together.
- **Reframes `feature-ws-hardening` AC-4** rather than closing it. Parent AC-4 promised "typed error frame", this spec promises "HTTP 404 + envelope". The frame-based variant is explicitly out of scope. The parent feature's findings should reflect that AC-4 is now permanently deferred-by-design, not deferred-pending-impl. Worth a re-evaluation note when this spec ships.
- **Builds on `feature-channels-and-messages`** (now merged) for the lookup helper. Spec asks for `repo.ChannelExists(ctx, id)` which doesn't exist yet; the implementer adds it as part of this spec. The existing `repo.GetChannel` (used by the messages handler at `apps/server/internal/http/messages_handlers.go:64`) is functionally equivalent but loads the full row; `ChannelExists` is the cheap-existence variant the WS path wants. Spec implementation note acknowledges the cost-of-a-DB-call-on-upgrade-hot-path concern and offers an LRU as a future mitigation.
- **Does NOT depend on `feature-security-headers-and-sqlite-ensure-wiring` or `feature-access-log-fields-and-wiring`**. The 404 envelope written here goes through `httpx.WriteError` directly; doesn't need the access log middleware to be wired. Means this can ship before either of those two follow-up specs.

### Cost-on-hot-path note worth flagging

Adding a DB call to the WS upgrade path is not free — even a SQLite local call adds ~100 µs per upgrade. At friend-scale (PRD §14: "single-process server with serialized writes") this is well under the noise floor. At any larger scale the LRU mitigation the spec mentions becomes load-bearing. Worth a follow-up plan or comment when scale changes.

## Recommendations

1. **No tests added by this run** — same call as the other planned-only stubs (PR #37 / PR #47 / PR #52 / PR #56): a guaranteed-failing red test for unimplemented code would put `pnpm test` in a permanent-fail state until the implementation PR ships. The findings doc + this PR body are the clearer signal.
2. When the implementation lands:
   - Verify `connState` struct exists with both fields populated post-redemption.
   - Verify the `ChannelLookup` is wired in main.go behind the same nil-skip pattern as `TicketStore`.
   - Verify `apps/server/internal/wsapi/handler.go:145` no longer contains `_ = userID` (the TODO removal AC).
   - Re-evaluate `feature-ws-hardening` findings: AC-3 partial → covered; AC-4 deferred → permanently-deferred-by-design (with a pointer to this feature for the actual closure).
3. **Cross-feature note for the next implementer:** the AC-1 binding plus the messages-handler reading half make a complete WS-attributed-message chain. Both halves shipping in one PR (or two coordinated PRs) closes a real user-visible gap — without it, messages broadcast over WS have no `user_id` attribution. PR #55 (channels-and-messages findings) noted this interlock; this spec is the ws-side resolution.
