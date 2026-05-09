# Phase 9 handoff

Date: 2026-05-09 07:06Z
Author: Claude (orchestrator session for phase-9 close-out)
Predecessor: `2026-05-08T1009Z-phase-8-handoff.md`

## Executive summary

**Phase 9 (`#860` — direct messages + cross-surface read state) drained.** All 39 sub-issues closed; 0 superseded, 0 closed-as-already-fixed. The original 15-sub-issue plan (#861–#875) ran to ground; the additional 24 (#911 / #917–#970, sparse) were filed mid-phase by reviewer + cold-pass passes and shipped before phase close.

Wall-clock from contract PR (#910 merged 22:10Z, 2026-05-08) to the last sub-issue PR (#972 merged 02:24Z, 2026-05-09) was ~4h15m. Diff across the 109 first-parent commits between the Phase 8 handoff (`fbf8531`) and HEAD: +11,275 / −2,302 over 202 files (includes one non-phase-9 cleanup, the `CHAT_SERVER_PORT` removal in #908 that was merged the morning of 2026-05-08).

The chat surface now ships 1:1 direct messages (six new HTTP endpoints, two new tables, one new WS frame `{type:"dm"}`), server-tracked read state for both channels and DMs (two `*_reads` tables, two `POST /read` endpoints, one new WS frame `{type:"read"}`, additive `last_read_message_id` + `unread_count` on `GET /api/channels`), and a multi-topic WS subscription model — every connection auto-subscribes to its `user:<viewer>` inbox topic alongside the existing channel topic so DM + read frames reach the right viewer without breaking the channel-scoped contract from earlier phases.

Run shape: contract-first (#910 PR merged alone), then six waves of parallel `phase-loop` (workers) + `pr-review-loop` (reviewers), plus a post-merge cold-pass review pass that filed 18 follow-up sub-issues against already-merged PRs. Peak concurrency 5 workers + 3 reviewers.

## What landed by area

### Contract (1 issue)

- **#861 PRD §4/§10/§11/§13 addendum + four `specs/plans/phase-9/*.md` feature specs** (PR #910) — pinned the DM endpoint set (`POST /api/dms`, `GET /api/dms`, `GET /api/dms/{id}/messages`, `POST /api/dms/{id}/messages`, `POST /api/dms/{id}/read`, `POST /api/channels/{id}/read`), the two new WS frame kinds (`{type:"dm"}`, `{type:"read"}`), the `user:<viewer>` inbox topic, and the asymmetric read-state init rule (channels auto-materialize on listing, DMs do NOT — NULL means "all peer messages unread"). Two new env vars listed: `CHAT_DM_WRITE_BURST` / `CHAT_DM_WRITE_REFILL` and `CHAT_READ_MARK_BURST` / `CHAT_READ_MARK_REFILL`. Eleven later implementation PRs ran in parallel against this fixed shape.

### Schema (1 issue)

- **#862 `0005_dms_and_read_state.sql`** (PR #915) — single migration covering all four new tables (`dm_conversations` with `user_a_id < user_b_id` canonical ordering, `dm_messages`, `channel_reads`, `dm_reads`) per L23 ("one migration per phase"). Indexes for the listing + unread-count hot paths landed in the same file.

### Server — infrastructure prep (3 issues)

- **#863 WS multi-topic + `user:<viewer>` auto-subscribe** (PR #916) — `apps/server/internal/wsapi/handler.go` upgrade path now subscribes the connection to BOTH the `?channel=` topic AND `user:<viewer>` simultaneously. `Hub.Subscribe` extended to take a slice of topic strings instead of a single channel ID.
- **#867 rate-limit buckets `dm-write` + `read-mark`** (PR #914) — pre-requisite for #870 + #871 to avoid `ratelimit/config.go` conflicts when DM-CRUD and DM-read landed in parallel (L17). `dm-write` defaults burst 10 / refill 60s; `read-mark` defaults burst 50 / refill 60s. Both per-user. #920 (PR #924) followed up with unit tests for the env-override paths.
- **#919 WS default-channel fallback (L15)** (PR #925) — `?channel=` omitted on `/ws` now resolves to `#general` in production (was a 400 + `testDefaultChannel = "#test-default"` constant gated on `cfg.ChannelLookup == nil`). Net: phase-9 web clients can connect with a single `/ws?token=…` and still receive channel frames via the auto-subscribed `user:<viewer>` inbox until they pick a channel. The contract's "Inferred (not verified at HEAD)" footnote in `specs/plans/phase-9/README.md` flagged this as the only "Flip if wrong" decision; the W sub-issue chose to land the production fallback rather than flip L15.

### Server — repo + transactions (2 issues)

- **#864 `InsertMessageTx` + channels denorm** (PR #922) — moved the channel-message insert path into a single transaction that also bumps `channels.last_message_id` + `channels.last_message_at`. Sets up the read-state denormalization shape that #868 + #869 build on.
- **#868 `channel_reads` repo + `MaterializeChannelReadsTx`** (PR #929) — `LastReadMessageID(channelID, viewerID)`, `MarkRead(channelID, viewerID, messageID)` (advance-only per L5), `MaterializeChannelReadsTx` for the lazy-init path (insert one zero-`last_read_message_id` row per (channel, viewer) pair on first listing). Mirrors the `auth_store.go:81` transaction pattern (L21).

### Server — DM CRUD (2 issues)

- **#870 DM CRUD + `InsertDMMessageTx` + WS dm frame** (PR #932) — `POST /api/dms` (idempotent `FindOrCreateDMConversation` per L18), `GET /api/dms` (per-viewer listing with `unread_count`), `GET /api/dms/{id}/messages` (paginated, immutable per L9), `POST /api/dms/{id}/messages` (rate-limited via `dm-write`, broadcasts `{type:"dm"}` to BOTH peers' `user:<viewer>` topics + writes the sender's `dm_reads` row atomically inside the same transaction).
- **#871 DM read state + `POST /api/dms/{id}/read` + WS read frame** (PR #936) — recipient-side mark-read endpoint, rate-limited via `read-mark`. Broadcasts `{type:"read", scope:"dm", id, last_read_message_id}` to the caller's `user:<viewer>` topic so other open tabs / devices see the read advance immediately (§7 cross-device sync).

### Server — channels read state (1 issue)

- **#869 channels listing extension + `POST /api/channels/{id}/read` + WS read frame** (PR #935) — `GET /api/channels` now returns `last_read_message_id` + `unread_count` per channel, materializing `channel_reads` rows on the hot path for first-time viewers (L11 asymmetric init: a new user sees 0 unread on a 50K-message channel, not 50K). `POST /api/channels/{id}/read` rate-limited via `read-mark`, broadcasts `{type:"read", scope:"channel", id, last_read_message_id}` to the caller's inbox.

### Wire types (2 issues)

- **#865 TS api-client DM types + `Event` extension + `Channel` optional fields** (PR #912) — `DMConversation`, `DMMessage`, `Read` types added to `packages/api-client/src/types.ts`; `Event` admits `DMMessageEvent` and `ReadEvent` arms; `Channel` gains optional `last_read_message_id` + `unread_count` fields. `// sync with packages/go-client/...` headers carried throughout. #917 (PR #0c12865) followed up with mock-fetch unit tests for the new wrappers.
- **#866 go-client DM types + `Event` extension + `Channel` optional fields** (PR #913) — mirror addition in `packages/go-client/dms.go` (new file) + extension to `channels.go`, `users.go`, `ws.go`. The `dms.go` file went on to require its own `// sync with` header (#962, PR `1069a4c`); the Go side surface is now seven hand-mirrored files (the CLAUDE.md "Wire types" parenthetical was updated through #966 + #970 to match — see "process notes" below).

### Web (3 issues)

- **#872 WS frame router for `dm` + `read` kinds + `useReadMarker` hook** (PR #943) — extended the shared `useChatSocket` consumer set to dispatch `{type:"dm"}` to a new `useDMs` hook and `{type:"read"}` to a new `useReadMarker` hook. `useReadMarker` debounces `POST /read` calls (250ms after last activity per L22), with a focus-flush on `visibilitychange`. #944 (PR #972) followed up with the Playwright spec for the debounce + focus-flush behaviour once #874 had wired the hook into the active channel.
- **#873 channels unread badges UI** (PR #950) — sidebar `ChannelsList` reads `unread_count` from the listing response, renders a count badge with a `99+` cap, drives `markRead` on active-channel selection. #954 (PR `94f4f42`) followed up by tightening the hook's nullable scope, switching `latestCommitted` to O(1) lookup, and pulling the badge text out as a literal. #964 (PR `eb1d1a3`) polished the comments + test names.
- **#874 DM feature UI** (PR #955) — DM sidebar section, DM thread viewer, "+ New DM" modal (reuses Phase 8 `Modal` primitive per L24), peer picker driven by a new `Client.ListUsers` typed method (#951 added the method, PR #957; the web side called it the same hour). #874 also forced #950's active-channel `markRead` to thread the channel ID into `usePresence` so the WS upgrade carries it (commit `0742b0b` — that commit predates the Phase 9 contract merge but the phase still leans on it).

### CLI (1 issue)

- **#875 `chatd dm` + `chatd channels read`** (PR #949) — six new sub-subcommands (`chatd dm list / send / history / read / watch / new`) plus `chatd channels read <id> <message-id>`. Locked-in shape per L25. #953 (PR `d7b9586`) followed up by surfacing unparseable WS frames on stderr in `dm watch` (was silently dropping); #952 (PR `1069a4c`) added the missing `// sync with` comment on the local `userSummary` mirror.

### Cold-pass review + reviewer follow-ups (18 issues)

A general-purpose agent re-read each merged PR against its feature spec + decision log + CLAUDE.md and filed concrete defects. Every one shipped before phase close:

- **#911** broken `§12` anchor in `dms.md` + missing materialization-choice record in `read-state.md` (PR #930).
- **#918** `Channel.last_read_message_id` wire-shape inclusion was ambiguous in the contract — resolved by including it in the listing wire-shape (PR #933).
- **#923** `makeFetch` test helper duplicated between `api-client` test files — extracted into a shared module (PR `0575c28`).
- **#926** doc-comment on `wiring.newDefaultChannelResolver` named the wrong concurrency primitive (PR `e0c7de2`).
- **#927** run-on parenthetical in `wsapi.Handler` doc-block split into two sentences (PR `8ea622f`). Both #926 and #927 came out of the same cold-pass scan of `wiring/` doc-comments.
- **#931** `ListChannelsWithReadState` doc-comment didn't spell out its precondition — tightened + added a precondition test (PR `1263972`).
- **#934** `validULID` helper rejected lowercase ULIDs in cursor + `peer_user_id` query params (PR `c7bc205`). Real bug: a lowercase URL-encoded ULID round-trip through the web client tripped the validator.
- **#937** `MaterializeChannelReadsTx` was being called even when the listing had zero new (channel, viewer) pairs to materialize — short-circuited on the hot path (PR `aad4034`).
- **#938** `unread_count` subquery in `ListChannelsWithReadState` carried a `COALESCE(..., 0)` defensive layer that became dead code once #937 proved materialize-on-listing always seeds the row (PR `4419459`).
- **#939** `userFromContext` unused-return drive-by — `//nolint:unparam` suppressed in #869 because the third return was kept "for future diagnostics" (lint commit `9cb5afc`); #939 dropped both the unused return and the `nolint` directive (PR `64e3c4a`).
- **#942** `{type:"read"}` frame on the channels surface used `channel_id` where the contract said `id` — aligned with `read-state.md` (PR `9ddeea2`). Real bug: the web `useReadMarker` ignored the frame entirely until this fix.
- **#947** `channel_reads` `Mark` handler accepted only uppercase ULIDs in the body (mirror of #934 on the request-body path; missed in the original cold-pass) (PR `fef4252`).
- **#951** `Client.ListUsers` typed method missing on go-client — used by #874 web new-DM modal but added late (PR `539c165`).
- **#952** `// sync with` comment on the local `userSummary` mirror missing in `apps/cli` (PR `1069a4c`, bundled with #962).
- **#953** `cli dm watch` silently dropped unparseable frames — surfaced on stderr instead (PR `d7b9586`).
- **#954** Web cleanup follow-ups from #950 (markRead nullable scope, latestCommitted O(1), badgeText literal) (PR `94f4f42`).
- **#962** `users.go` missing in the TS api-client `// sync with` comment list — added (PR `1069a4c`, bundled with #952).
- **#964** `useReadMarker` comment + test-name polish + `ChannelsList` badge cap sync (PR `eb1d1a3`).
- **#966** + **#970** CLAUDE.md "Wire types" parenthetical drift — the original `(five envelope types)` phrasing predated Phase 9. #966 expanded the file list to the post-DM/users set; #970 dropped the count entirely so it can't re-stale (PRs `351a19c` + `d38f5ed`).

Bundled PRs: #952 + #962 (both `// sync with` comment additions, single PR `1069a4c`); #966 + #970 (sequential — #966 corrected the file list, #970 then dropped the now-likely-to-stale count).

## Verification at handoff (`origin/main` HEAD = `6c37b80`)

- `pnpm run lint` — clean
- `pnpm -r typecheck` — clean
- `pnpm run format:check` — clean
- `pnpm run check:workspace-exports` — ok
- `pnpm --filter ./apps/web test` (vitest) — green; new `useReadMarker` + DM thread / sidebar / new-DM modal cases included
- `pnpm e2e:web` (Playwright, container runner) — green; web-read-marker debounce + focus-flush spec passes (#944)
- `go test ./apps/server/... ./tests/...` — green; new `tests/e2e/phase-9/` suites pass (DM round-trip + read-state cross-device + WS multi-topic + multi-client wire-drift canary on the new envelope kinds)
- `golangci-lint run ./apps/server/... ./packages/go-client/...` — zero issues
- `bash scripts/smoke.sh` — green
- `bash scripts/smoke-docker.sh` — green (the in-image smoke from Phase 7 still drives `docker compose up --build` end-to-end with the Phase 9 surface in the binary)

The above are observed in the orchestrator's main-tree at the close-out commit. Per repo convention CI is the source of truth — the corresponding GitHub Actions runs for the listed PRs were green at merge.

## Process notes

1. **Contract-first PR (#910) gated five parallel implementation PRs in Wave 2.** PRD addendum + the four feature specs landed alone; once merged, #912/#913/#914/#915/#916 opened in parallel against a frozen wire shape and merged within ~25 min of each other without a single rebase. Same lesson Phase 8 confirmed in the opposite direction (visual-coupled diffs prefer one branch; type-coupled diffs prefer a frozen contract + parallel land). At Phase 9 scale (39 sub-issues, 11K+ insertions), the contract-first pattern paid off again — the only mid-phase contract amendments were #918 (`last_read_message_id` listing inclusion) and #911 (anchor + materialization-choice record), both docs-only and neither blocked downstream work.

2. **Cold-pass review pattern caught real defects, again.** A general-purpose agent re-reading each merged PR against its spec + decision log filed 18 sub-issues against already-merged code. Of those, 5 were genuine bugs (#934 + #947 lowercase-ULID validators, #942 wrong field name on channels read frame, #951 missing typed method that #874 already depended on, #953 silently-dropped CLI frames); the rest were doc-comment / test-name / dead-code drift. The "doc-comment / dead-code drift" findings are the kind of thing that worker + reviewer pairs stop noticing once both are in the spec's frame; cold-pass keeps catching them. Worth keeping the pattern as a phase-close ritual.

3. **CLAUDE.md "Wire types" parenthetical re-staled twice in this phase alone.** Phase 7 #809 introduced the `(five envelope types)` count when there were five `packages/go-client/*.go` production files. Phase 9 added two more (`dms.go`, `users.go`); #966 expanded the list, #970 dropped the count entirely. The repeated drift confirms that any "N items" phrasing in CLAUDE.md will re-stale on the next phase that adds an item; the fix is to drop the number and let the discovery mechanism (`grep -r 'sync with' packages/`) be the source of truth.

4. **Two parallel "user color / read state" rate-limit buckets shipped via the dedicated predecessor PR pattern (L17, mirror of Phase 8 #837).** #867 landed `dm-write` + `read-mark` config alone, before #870 + #871 opened. Zero `ratelimit/config.go` conflicts during Wave 5; the alternative would have been three-way conflicts on the same const block.

5. **Asymmetric read-state init was the right call.** Channels auto-materialize on `GET /api/channels` so a new user sees 0 unread instead of 50K (per L11); DMs do NOT — NULL `last_read_message_id` means "all peer messages unread", which is correct for DMs that arrived while the recipient was offline. The web side handles the NULL case explicitly in `useReadMarker` (PR #943) and the unread-count math in #871. The asymmetry is documented in `read-state.md`; the cold-pass review (#911) added the materialization-choice record so the next reader doesn't have to re-derive it from the migration.

6. **#919 chose to land the production `defaultChannel = "#general"` fallback rather than flip L15.** Phase 9's contract surfaced this as the one decision the contract knew was inconsistent with HEAD. The W sub-issue verified that flipping L15 would have forced every Phase 9 client to pass `?channel=…` even when the multi-topic auto-subscribe meant they could otherwise connect channel-less and still receive `user:<viewer>` frames. Landing the fallback removed that contortion. Spec line 65 of `specs/plans/phase-9/README.md` is the trace.

## Open follow-ups

None against Phase 9. Every sub-issue in the native sub-issue list closed before phase close.

One spec-linkage worth flagging for future renames: `specs/plans/phase-9/ws-routing.md` and `dms.md` reference `Hub.Subscribe(topics []string)`, `wsapi.Handler.upgrade`, and `wiring.newDefaultChannelResolver` by name. The shipped server uses those exact symbols; if a later refactor renames any of them, update the spec in the same PR rather than leaving the spec referring to a removed symbol. (Same convention Phase 8 noted for `Hub.BroadcastChannel`.)

## Numbers

- Sub-issues at phase open: 15 (the original implementation set #861–#875)
- Sub-issues opened during phase: 24 (cold-pass + reviewer follow-ups: #911, #917–#920, #923, #926, #927, #931, #934, #937–#939, #942, #944, #947, #951–#954, #962, #964, #966, #970)
- Sub-issues closed in Phase 9: 39
- Epics closed: 1 (#860)
- PRs merged in Phase 9: 27 numbered (#910 / #912–#916 / #922 / #924–#925 / #929–#930 / #932–#933 / #935–#936 / #940 / #943 / #946 / #948–#950 / #955 / #957 / #959 / #967–#968 / #972) plus the 12 cold-pass commits that landed via the same workflow
- Diff across non-diary commits since Phase 8 close (`fbf8531..HEAD`): +11,275 / −2,302 over 202 files, 109 first-parent commits (includes the small non-phase-9 #908 `CHAT_SERVER_PORT` cleanup)
- Cold-pass review defects filed post-merge: 18 (#911, #917–#918, #923, #926–#927, #931, #934, #937–#939, #942, #944, #947, #952–#954, #962, #964, #966, #970); all shipped before phase close
- Wall clock from contract-PR merge (#910 22:10Z 2026-05-08) to last sub-issue PR merge (#972 02:24Z 2026-05-09): ~4h15m
