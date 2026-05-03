# Phase 2 handoff

Date: 2026-05-03 21:40Z
Author: Claude (orchestrator session for phase-2 build-out)
Predecessor: `2026-05-03T1755Z-parallel-pr-churn-lessons.md` (parallel-PR-churn lessons mid-session)

This is the operational handoff for what landed in Phase 2 (web UI + shared clients + presence + sec-audit closeout) during the 2026-05-03 session. Reflective lessons live inline below — there's enough new this session that pulling them into a separate file would just split the audit trail.

## What landed

### Five feature tracks (the original sub-issues)

- **#64** — `packages/go-client`: HTTP+WS client used by CLI. ULID-aware, ws-ticket-mint flow, body cap reads (1 MiB) and proper response-body close on the WS upgrade path.
- **#66** — `apps/cli` full command set: `register`, `login`, `whoami`, `channels` (+ `create`), `send`, `history`, `watch`, `logout`. Token cached at `~/.chatd/token`. **Built with stdlib `flag`**, not cobra (PRD revision #128 documents this; cobra would be a future migration if subcommand groups / completion become valuable).
- **#67** — `packages/api-client` (TS): HTTP+WS client + shared types. `WebSocketClient` exposes `transition` events (added later under #110) and a static `observe()` for cross-instance recording.
- **#68** — `apps/web`: Vite + React 18 + TypeScript chat page. Auth context, channels list, messages list, presence rail. **State via React Context, styling via plain CSS** (PRD #128 documents the deviation from the originally-named Zustand+Tailwind stack — neither was load-bearing at 4-route scale).
- **#69** — Server-side presence: `Hub.AddPresence` / `RemovePresence` + `BroadcastAll`-driven join/leave delta frames. `GET /api/presence` REST endpoint for snapshot seeding (newly documented in PRD §10 by #128).

### Sec-audit closeout (the #78 umbrella, all 8 findings)

- **PR #87** — `/debug/subs` gated to loopback (medium #1).
- **PR #86** — Access log writes the auth-package ctx-key user_id (medium #2).
- Commit `92d447f` — Drop raw inbound WS rebroadcast; sender-spoofing surface eliminated (medium #3).
- **PR #92** — WS ticket consumed only after channel-existence check (low #4).
- **PR #96** — Startup `WARN` when `CHAT_ALLOW_PUBLIC_BIND=1` is set without a trusted-proxy parser (low #5; full `CHAT_TRUSTED_PROXY` parser remains deferred to PRD §9).
- **PR #93** — `http.Server.MaxHeaderBytes` capped at 16 KiB (low #6).
- **PR #101** — `wsapi` upper-folds `?channel=<id>` via shared `ids` normalizer; matches REST handler (info #7).
- **PR #100** — Forward migration `0002_messages_cursor_index.sql` adds explicit `(channel_id, id)` index (info #8).

#78 closed with a recap table.

### Process / quality items

- **PR #94** (#90) — ESLint errors block CI. `pnpm run lint` runs with `--max-warnings 0`; new `scripts/check-workspace-exports.mjs` verifies in-repo TS packages resolve from source (no `./dist/` references) so the lint job can never go green by accident on a missing build.
- **PR #102** (#81) — Phase-2 e2e suite under `tests/e2e/`. CLI scenarios via vitest spawning the real `chatd` binary; web scenarios via Playwright + chromium against the Vite-built `apps/web/dist` fronted by a tiny same-origin Node proxy. New CI `e2e` job. Two ACs deferred and tracked as #103 (presence assertion) + #104 (WS drop+restore).

### Follow-up cluster (gaps surfaced during Phase 2 itself)

- **PR #105** (#103) — Web presence list rendering: `apps/web/src/hooks/usePresence.ts` + `data-testid` hooks in `Chat.tsx` so the e2e presence assertion can become real.
- **PR #106** (#104) — Web WS drop+restore e2e re-enabled via Playwright `routeWebSocket`. Replaced the brittle DOM-text polling with an api-client `transition` event log; un-skipped 10/10 consecutive runs.
- **PR #115** (#108) — `useMessages` refetches recent history on WS reopen and merges by id, surfacing messages posted during a transient drop without a page reload.
- **PR #111** (#109) — `tests/e2e/playwright/proxy.mjs` attaches `error` listeners on both halves of the upgraded socket pipe; ECONNRESET on browser teardown no longer crashes the runner.
- **PR #119** (#110) — Replaced the WS-drops e2e flake by asserting against `window.__chatd.wsTransitions` (recorded via `WebSocketClient.observe`) instead of polling the badge.
- **PR #120** (#114) — Env-overridable `RegisterIPConfig` (`CHAT_REGISTER_BURST` / `CHAT_REGISTER_REFILL`); production default holds; e2e fixture sets a generous budget so a 4th register no longer 429s.

### CLI fixes from independent E2E coverage (#107)

- **PR #116** (#112) — `Env` caches a `*bufio.Reader` once so `chatd register <user> <<< $'pw\\ninvite\\n'` reads both prompts. Fixed two failing E2E tests in `phase-2/cli-full-commands`.
- **PR #117** (#113) — `apps/cli/cmd/history.go` pre-splits args into (positional, flag) before `flag.Parse`, so `chatd history <channel> --limit 2` works alongside the pre-existing `chatd history --limit 2 <channel>`. Custom `splitFlagsAndPositional` helper covers `--name=value`, single-dash form, `--` terminator, and bool flags.

### Tooling & spec alignment

- **#118** (closed with recap) — Tracking issue for the `isolation: "worktree"` Edit/Write leak. Most mitigations shipped this session (worker §0 preflight, parent-status check, supervisor failed-agent guard, truncation handling). Remaining options (settings.json path-filter, true chroot) are harness-level and deferred.
- **PR #128** (#126) — PRD revision aligning the spec to implemented reality across §7 (web stack, CLI framework), §8 (tech-stack tables), §10 (WS inbound protocol, presence frame, new `GET /api/presence` doc), plus a "Design deviations from earlier PRD revisions" section documenting the four divergences with PR/commit refs. Followed by **commit `610aa1c`** dropping a self-referential SHA from the revision header.
- **PR #129** (#125) — Web optimistic render on `useMessages.send()`: synthetic `pending-<uuid>` entry on submit, reconcile in place when the server's WS echo arrives, mark `failed` with retry affordance on REST POST error. The only PRD-as-written code gap from the original gap analysis (#121-#125) — the other four were closed as PRD-update cases under #126.

## Operational lessons (this session)

The mid-session lessons file (`2026-05-03T1755Z-parallel-pr-churn-lessons.md`) covers the early Phase-2 churn. The following are new and codified into the agent / skill files:

### Worktree-isolation leakage

`isolation: "worktree"` was observed to leak Edit/Write into the parent project root in three incidents (agents `ae09b7f8`, `ab6d3b68`, `a4347ee4`). Caught by `git status` in the parent root, surfaced to the running worker via `SendMessage`, recovered by copy + scrub + re-CI before push.

Codified as:
- `.claude/agents/issue-pr-worker.md` §0 — mandatory `pwd` + `rtk git rev-parse --show-toplevel` first tool call; absolute worktree-rooted paths for every Edit/Write; both-side `git status` check before commit.
- `.claude/skills/phase-loop/SKILL.md` step 11 — failed-agent worktrees have uncommitted WIP that `worktree remove -f -f` destroys; capture WIP before cleanup.
- Memory `feedback_subagent_path_leakage.md`, `feedback_failed_agent_no_cleanup.md`.

### Truncated agent reports

The harness's monitor sometimes truncates the agent's final §9 report ("Waiting for the monitor."). The supervisor falls back to verifying state directly via `gh pr view`. Codified in `.claude/skills/phase-loop/SKILL.md` step 10.

### Lint/test cache pollution from sibling worktrees

A worker stalled 600s on `golangci-lint` because its cache referenced an already-removed sibling worktree. Codified in `.claude/agents/issue-pr-worker.md` §5: run `golangci-lint cache clean && go clean -testcache` from inside the worker's own worktree before the local CI mirror.

### Auto-file follow-ups

Workers now file new GitHub sub-issues on the parent epic for any defect/skip outside their PR's footprint (instead of letting them rot in the structured report). The follow-up is also linked as a native sub-issue via `POST /repos/.../issues/<epic>/sub_issues` with `-F sub_issue_id=<id>` (the API rejects string IDs). §8 of `.claude/agents/issue-pr-worker.md`.

This fired five times this session — #108, #109, #110, #114, and the larger gap-analysis cluster (#121-#125 → #126). Without this, those defects would have been quietly absorbed into PR-body footnotes and lost.

### `in-review` is a cross-process lock

The new `pr-review-loop` skill (rewritten this session to mirror `phase-loop`'s shape) treats the `in-review` label as a lock that another reviewer (this loop on a different machine, a human, a different agent) may hold. Stripping it without proof of ownership "steals" PRs from active reviewers. Codified in `.claude/skills/pr-review-loop/SKILL.md` step 3, memory `feedback_no_pr_merging.md` (existing) + the strict-eligibility commit `4545254`.

### Epic-close gates on merge, not on PR-open

A late lesson: I almost closed Phase-2 the moment the last sub-issue's worker reported MERGEABLE. PR-open + green CI is not the same as merged-on-main, and `Closes #N` linkage only fires on actual merge. Codified in memory `feedback_epic_close_gates_on_merge.md`.

### Native sub-issue links

The `Parent: #N` text convention is necessary but not sufficient — GitHub also has a native sub-issues feature (`POST /repos/.../issues/<n>/sub_issues`) that the epic's UI surfaces. Worker §8 now attaches both the textual reference and the native link.

REST `GET /sub_issues` returns insertion order, not priority order — the priority API returns 204 but the listing endpoint doesn't reflect it. The textual `## Sub-issues` list in the epic body remains the durable source of truth for ordering.

## PR / agent map

Numbers in chronological merge order:

| PR | Closes | Headline |
|----|--------|----------|
| #80 / #84 / #87 / #86 / #88 | various | Phase-2 feature tracks (#64, #66, #67, #68, #69) — landed earlier in session |
| #92 | sec #4 (refs #78) | WS ticket post-channel-check |
| #93 | sec #6 (refs #78) | MaxHeaderBytes cap |
| #94 | #90 | ESLint blocks CI + workspace-exports check |
| #96 | sec #5 (refs #78) | Trusted-proxy startup WARN |
| #100 | sec #8 (refs #78) | messages cursor index migration |
| #101 | sec #7 (refs #78) | wsapi case-fold |
| #102 | #81 | e2e suite |
| #105 | #103 | web presence list |
| #106 | #104 | WS drop+restore via routeWebSocket |
| #111 | #109 | proxy ECONNRESET handlers |
| #115 | #108 | useMessages reopen catchup |
| #116 | #112 | CLI scripted-stdin prompt |
| #117 | #113 | CLI history flag-after-positional |
| #119 | #110 | WS-drops flake fix via transition log |
| #120 | #114 | env-overridable register-IP budget |
| #128 | #126 | PRD revision (closes 4 gap analyses) |
| #129 | #125 | web optimistic send |

Plus drive-by fixes from the test-analysis loop and structural cleanups merged as part of the Phase-2 wave but not directly closing a Phase-2 sub-issue (#107 etc.).

## What's next

Phase-3 (#63: Polish, demo) opens next. Audit issue **#127** is the first sub-issue and gates the rest — it re-reads each Phase-3 issue against the implemented Phase-2 reality (since they were drafted against the original PRD that #126 just superseded). Run #127 before any other Phase-3 work; expect updates to #70 (README — no Tailwind/cobra), #71 (embedded build — verify against actual Vite output), #99 (history reverse-chrono — depends on the merged `useMessages` ordering, including the optimistic-send patch from #129).
