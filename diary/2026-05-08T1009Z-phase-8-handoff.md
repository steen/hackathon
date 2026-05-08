# Phase 8 handoff

Date: 2026-05-08 10:09Z
Author: Claude (orchestrator session for phase-8 close-out)
Predecessor: `2026-05-07T2015Z-phase-7-handoff.md`

## Executive summary

**Phase 8 (`#834` — channel lifecycle: create + rename) drained.** All 20 sub-issues closed. 18 PRs merged between #876 (contract-only, 07:38Z) and #905 (cross-client WS drift canary, 10:01Z). Diff across implementation commits: +5477 / −200 over 20 non-diary commits.

The phase added a server-side rename endpoint, a per-user channel-write rate limit, a typed `channel` WS broadcast (kinds `create` / `rename`), an accessible `Modal` primitive, a shared `useChatSocket` provider, channel create + rename UI in the web app, `chatd channels create|rename` sub-subcommands in the CLI, and the corresponding TS + Go client extensions. Contract-first: PR #876 landed PRD §7/§9/§10/§11 + the eight `specs/plans/phase-8/*.md` feature specs as a docs-only gate before any implementation PR opened.

Run shape: parallel `phase-loop` (workers) + `pr-review-loop` (reviewers) + a post-merge cold-pass review pass that filed five additional sub-issues (#898–#902) against already-merged PRs. Wall clock ~2h30m from #876 merge to #905 merge.

## What landed by area

### Contract (1 issue)

- **#835 PRD §7/§9/§10/§11 + 8 feature specs** (PR #876) — pinned wire shapes for `PATCH /api/channels/{id}`, the outbound `channel` WS frame (`kind: "create" | "rename"`), `CHAT_CHANNEL_WRITE_BURST` + `CHAT_CHANNEL_WRITE_REFILL` env vars, US-13 (rename happy + 403/#general + 409/dup + 429/ratelimit). Inbound `channel` frames remain forbidden — the sender-spoofing rule from earlier phases is unchanged. Five later implementation PRs ran in parallel against this fixed shape.

### Server (3 issues)

- **#837 rename endpoint + per-user channel-write limit + WS broadcast** (PR #880) — `PATCH /api/channels/{id}`, per-user token bucket (configured via `CHAT_CHANNEL_WRITE_BURST` / `CHAT_CHANNEL_WRITE_REFILL`, defaults 5 / 60s), `Hub.BroadcastChannel(kind, ch)` entry point used by the create + rename handlers. Refused renames on `#general` (403) and on duplicate names (409) per US-13.
- **#883 audit channel-write 429s in `auth_events`** (PR #887) — rate-limited writes now emit a `channel_write_ratelimited` audit row with `actor_id`, `channel_id`, and `kind` so the limiter is observable in the same table as auth refusals.
- **#884 info-log effective limiter config on startup** (PR #891) — single `slog.Info("channel-write limiter active", "burst", N, "refill", D)` line at boot so operators can confirm the env-override took effect without grepping the rate-limit table.

### Wire types (2 issues)

- **#840 `api-client` renameChannel + `ChannelEvent`** (PR #877) — typed function + interface in `packages/api-client/src/types.ts`; carries the `// sync with packages/go-client/...` header.
- **#841 go-client `RenameChannel` + typed `channel` event decoding** (PR #879) — added to `packages/go-client/channels.go` + decoder branch in `ws.go`, paired with the same sync comment.

### Web (4 issues)

- **#836 accessible Modal primitive** (PR #878) — keyboard-first dialog in `apps/web/src/components/Modal.tsx` with focus trap (Tab / Shift-Tab wrap) and Escape-dismiss.
- **#842 lift WS connection to a shared `useChatSocket` hook** (PR #881) — single ws subscription owned by the chat shell; `useMessages` and `useChannels` consume the same connection instead of opening their own. Drops a class of double-subscribe bugs the moment a third consumer is added.
- **#844 channel create + rename UI** (PR #892) — `ChannelCreateModal`, `ChannelRenameModal`, `useChannels` create + rename actions, wired against the `channel` WS frame so the sidebar updates the moment the server confirms.
- **#845 CLI sub-subcommands** (PR #886) — `chatd channels create <name>` and `chatd channels rename <id> <new-name>`, both following PRD §7's `<id>\t<name>` output convention.

### Cold-pass review follow-ups (5 issues)

After the original eight implementation PRs merged, a general-purpose agent re-read each merged PR against its feature spec and CLAUDE.md and filed concrete defects. Every one shipped before phase close:

- **#898** `Retry-After` on channel-write 429 used the burst constant instead of `CHAT_CHANNEL_WRITE_REFILL` — refill override was silently ignored. (PR #904.)
- **#899 / #900 / #901 — three Modal a11y findings bundled into one PR** (#903): Modal didn't `createPortal` into `document.body` (would have been trapped by any future ancestor `transform`); backdrop dismiss used `click` (drag-select-out-and-release dismissed the rename modal); Tab / Shift-Tab focus trap had no test coverage even though the feature spec explicitly required it. Bundled deliberately because all three touched `Modal.tsx` + `Modal.test.tsx` — serializing them would have meant two rebases against each other for no review-signal gain.
- **#902 missing wire-drift e2e canary** — CLAUDE.md "Wire types" mandates a `tests/e2e/` assertion for every hand-mirrored Go ↔ TS pair. The new `ChannelEvent` type shipped in #877/#879/#880 without one. (PR #905 added a two-client canary: one Go, one TS, both subscribe; one issues `POST /api/channels` then `PATCH /api/channels/{id}`; both must observe `channel` frames with `kind` `create` and `rename`.) The original implementation tickets shipped the type but missed the canary requirement — the spec called for it (`80-feature-clients-channel-extensions.md` line 39-45 + AC line 53), but the cold-pass review was where it actually got caught.

### Hardening / cleanup (5 issues)

All filed by `pr-reviewer` agents and shipped before phase close:

- **#882 Modal optional props** — `closeOnBackdrop?: boolean` and `initialFocusRef?: RefObject<HTMLElement>` (PR #889).
- **#885** drop unused `BACKOFF_MS` from `useMessages.helpers.ts` (PR #888).
- **#890** test guarding `apps/cli` and `apps/server` channel-name regexes against silent drift (PR #893).
- **#894** drop redundant cast in `useChannels` WS handler in favor of a typed guard (PR #897).
- **#895** wrong test-path reference in the channel-name regex alias comment (PR #896).

## Verification at handoff (`origin/main` HEAD)

- `pnpm run lint` — clean
- `pnpm -r typecheck` — clean
- `pnpm run format:check` — clean
- `pnpm run check:workspace-exports` — ok
- `pnpm --filter ./apps/web test` (vitest) — green (Modal.test.tsx 18 cases incl. portal mount, pointerdown semantics, Tab / Shift-Tab wrap, outside-panel snap-back)
- `pnpm e2e:web` (Playwright, container runner) — green
- `go test ./apps/server/... ./tests/...` — green; new `tests/e2e/` cross-client channel-frame canary passes
- `golangci-lint run ./apps/server/...` — zero issues
- `bash scripts/smoke.sh` — green

## Process notes

1. **Contract-first PR (#876) gated five parallel implementation PRs.** PRD §7/§9/§10/§11 + the eight feature specs landed alone; once merged, #877/#878/#879/#880/#886 opened in parallel against a frozen wire shape and merged within ~30 min of each other without a single rebase. The lesson Phase 6 taught the other way (single long-lived branch when the diff is visually-coupled) and Phase 8 confirms in the opposite direction: when the diff is type-coupled rather than visual, freezing the contract first lets the rest of the work parallelize cleanly.

2. **Cold-pass review pattern caught real defects.** A general-purpose agent re-reading each merged PR against its spec + CLAUDE.md filed five concrete sub-issues against already-merged code (#898–#902). All five were real (rate-limit math bug, three Modal a11y issues, a missing wire-drift canary that CLAUDE.md explicitly mandates) — none were nits. Worth keeping the cold-pass step as a phase-close ritual: the workers and reviewers were both green-stamping a spec they shipped against, but a fresh reading caught what the implementer-reviewer pair stopped noticing.

3. **Bundling the three Modal a11y findings into one PR (#903) avoided rebase serialization.** All three touched `Modal.tsx` + `Modal.test.tsx`; opening three PRs would have meant two of them rebasing against the merged predecessor for no review-signal gain. The reviewer accepted the bundle on the explicit "same two files, independent enough to land together" rationale stated in the PR notes.

4. **The implementation tickets shipped the wire type without the canary CLAUDE.md mandates.** `specs/plans/phase-8/80-feature-clients-channel-extensions.md` AC line 53 spelled the e2e canary out, and CLAUDE.md "Wire types" makes it a repo-wide rule. PR #877 + #879 + #880 all merged without one; the cold-pass review caught it as #902. Worth adding "if you added or changed a Go ↔ TS wire type, list the e2e canary path in the PR body" to the worker / reviewer prompts so the rule is enforced at PR-open instead of post-merge.

## Open follow-ups

None against Phase 8. Every sub-issue closed before phase close.

One spec-line is locked in by the merged decision and worth flagging for any future rename of the WS broadcast entry point: `specs/plans/phase-8/30-feature-channel-ws-events.md` lines 20, 24, 26, 27 reference `Hub.BroadcastChannel(kind, ch)` by name. The shipped server (`apps/server/internal/hub/...`) uses that exact signature; if a later refactor renames it, update the spec in the same PR rather than leaving the spec referring to a removed symbol.

## Numbers

- Sub-issues at phase open: 8 (the original implementation set #835/#836/#837/#840–#842/#844/#845)
- Sub-issues opened during phase: 12 (4 reviewer follow-ups, 5 cold-pass follow-ups, 3 Modal-related: #882/#883/#884 surfaced from spec-review of #837 + #836)
- Sub-issues closed in Phase 8: 20
- Epics closed: 1 (#834)
- PRs merged in Phase 8: 18 (#876–#905, see merged list above)
- Diff across non-diary commits: +5477 / −200 over 20 commits
- Cold-pass review defects filed post-merge: 5 (#898–#902); all shipped before phase close
- Wall clock from contract-PR merge to phase-close: ~2h30m
