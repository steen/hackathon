---
feature: cli-send-watch
phase: phase-0
analyzed_at: 2026-05-03T20:38:00Z
analyzed_commit: a283ba3df1d16750dfe0ccbd8e4f370dd6519c68
implementation_status: implemented
total_acs: 4
covered: 3
partial: 1
missing: 0
deferred: 0
---

# Test analysis: CLI `chatd send` and `chatd watch` (no auth)

**Spec:** `specs/plans/phase-0/feature-cli-send-watch.md`
**Implementation status:** implemented (with audit #78 functional regression on AC-1) — `apps/cli/cmd/{send,watch,url}.go` provide the public `Send`, `Watch`, and `ResolveURL` functions; `apps/cli/main.go` wraps them as the `chatd` binary. **Audit #78 (PR #85) silently functionally-regressed `chatd send`:** the binary still writes a frame to `/ws`, exits 0, and its in-package test still passes — but the server now silently DROPS inbound WS frames as of `92d447f` (raw rebroadcast removed because peers could forge `sender_user_id` envelopes). End-to-end nothing reads the frame. The smoke script was rewired (`cf82cdc`, `142e8b7`) to use `POST /api/channels/{id}/messages` instead, bypassing `chatd send` entirely.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success. | partial | `apps/cli/cmd/send_test.go::TestAC_0_1_SendWritesSingleTextFrameToWebSocket` (asserts a frame is *written* — passes) + `tests/cli-send-watch/cli_test.go::TestAC1_CliSendWatch_SendWritesPayloadAsTextFrameAndExitsZero` (asserts the binary exits 0 — passes). **However:** post-audit-#78 the server drops the frame at `apps/server/internal/wsapi/handler.go::readLoop` (commit `92d447f`). The CLI's exit-0 + frame-written contract is still satisfied; the *system-level* "the message reaches subscribers" contract is broken. The spec's AC text only pins what the CLI does, not what the server does with it — so on a strict reading the AC is technically satisfied. Marking partial because the load-bearing intent ("send delivers a message that watchers will see") is broken even though the literal AC text reads as satisfied. |
| AC-2 | `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line. | covered | `apps/cli/cmd/watch_test.go::TestAC_0_2_WatchPrintsEachFrameOnItsOwnLine` + `tests/cli-send-watch/cli_test.go::TestAC2_CliSendWatch_WatchPrintsEveryReceivedFrameOnePerLine`. Note: the watcher itself is fine — anything `hub.Broadcast` fans out reaches the watcher; only the *raw inbound rebroadcast* path (used by `chatd send`) was removed, not the broadcast path itself. |
| AC-3 | Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`. | covered | `apps/cli/cmd/url_test.go` (3 tests covering flag-over-env-over-default) + `tests/cli-send-watch/cli_test.go::TestAC3_CliSendWatch_UrlPrecedenceFlagOverEnvOverDefault` |
| AC-4 | No login flow or token handling exists in this phase. | covered | `apps/cli/cmd/no_auth_test.go::TestAC_0_4_NoAuthSymbolsReferencedFromCLI` (static walk of `apps/cli/**/*.go`) + `tests/cli-send-watch/cli_test.go::TestAC4_CliSendWatch_NoAuthorizationHeaderOnUpgrade` (runtime assertion). The CLI doesn't pass bearer tokens; for the new ticket flow it accepts `--ws-ticket TICKET` and appends `?ticket=<hex>` to the URL — that's not auth in the bearer-token sense, just opaque-token forwarding. |

## Findings

### Partial — AC-1 silent functional regression

`apps/cli/cmd/send.go` (unchanged): dials `/ws`, writes one text frame, performs a clean close handshake, exits 0. The in-package test pins exactly that.

What changed under it: `apps/server/internal/wsapi/handler.go::readLoop` post-#78 reads the frame, enforces size + rate limits, and then **silently discards the bytes**. The old `h.Broadcast(channel, data)` call is gone (commit `92d447f`). So the observable end-to-end behavior is:

- Before #78: `chatd send "hi"` → frame on the wire → server rebroadcasts → all `chatd watch` subscribers print `hi`.
- After #78: `chatd send "hi"` → frame on the wire → server drops → no subscriber sees anything.

The CLI's local test `TestAC_0_1_SendWritesSingleTextFrameToWebSocket` only checks the wire, not the broadcast — so it doesn't fail. The smoke script `scripts/smoke.sh` was already rewired in `cf82cdc` to use `POST /api/channels/{id}/messages` instead, so the system-level smoke flow doesn't catch the regression either (it bypasses `chatd send`).

**No failing test added by this run.** Reasoning:

- Adding a system-level test that drives `chatd send` then asserts a `chatd watch` peer receives the frame would be permanently red, blocking `pnpm test`. That's not the right pressure to apply — the regression is intentional (audit fix), not accidental.
- The right resolution is **production code change, out of test-agent scope:** either rewrite `chatd send` to POST against `/api/channels/{id}/messages` (requires auth wiring on the CLI) and update the spec, OR deprecate `chatd send` outright and update the spec to remove AC-1.
- Both options touch `20-feature-cli-full-commands` (which adds auth-aware CLI commands). When that ships, this AC should re-evaluate.

The CLI itself is honest about this — `apps/cli/main.go:5` documents the `--ws-ticket TICKET` flag, anticipating the auth-aware future.

### AC-2 still works because broadcast still works

The watcher subscribes via `/ws?channel=<X>` and waits for frames. `hub.Broadcast(channel, msg)` is unchanged — REST-driven broadcasts (from the messages handler) reach the watcher. Presence frames (from `hub.BroadcastAll`) reach the watcher too. Only the WS-inbound-rebroadcast path was removed.

### Cross-feature observations

- **Smoke script rewiring** (`cf82cdc`, `142e8b7`): the smoke flow is now `register → login → ws-ticket → channel-create → POST /api/channels/{id}/messages → watcher receives`. `chatd send` is no longer used. The wiring vitest was updated (`a9222d2`, `607ca1e` for prettier) to assert the new shape.
- **`feature-server-ws-hub` AC-3 contract change** (see `phase-0/server-ws-hub.md`): the same audit #78 fix is the root cause of this regression. Both findings docs should be read together.
- **`apps/cli/main.go` already accepts `--ws-ticket`.** A future PR could add `chatd send-message --channel <id> <text>` that POSTs against `/api/channels/{id}/messages` using the same ticket+JWT machinery; that would close this AC properly. Out of scope for this tick.

## Recommendations

1. No new tests added by this run. Adding a permanently-failing system test would block CI without giving the maintainer new information; the partial flag + this findings doc are the clearer signal.
2. **Production change for AC-1 (out of test-agent scope):** decide whether `chatd send` should be deprecated or rewritten to use the REST POST path. The `20-feature-cli-full-commands` spec is the natural home for the rewrite.
3. **Spec follow-up (out of test-agent scope):** AC-1's wording ("connects to `/ws`, sends one text frame") names a specific protocol-level action. If the production decision is to rewrite, the spec should pin "produces a message that subscribers receive" and let the implementation choose REST vs WS.
4. When `20-feature-cli-full-commands` lands and `chatd send` is fixed (or removed), re-evaluate at the new SHA to flip AC-1 partial → covered (or to drop it from the AC list).
