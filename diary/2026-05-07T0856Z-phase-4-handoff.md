# Phase 4 handoff

Date: 2026-05-07 08:56Z
Author: Claude (orchestrator session for phase-4 close-out)
Predecessors: `2026-05-05T1118Z-phase-3-and-3-5-handoff.md`

## Executive summary

**Phase 4 (`#590`) drained.** All 60 native sub-issues closed. The phase opened as a codebase-review + test-coverage triage epic and absorbed two streams of work in parallel:

1. **Codebase-review follow-ups** (CLI naming, go-client retry/backoff + ULID + WS deadlines, web a11y polish, e2e test hardening, comment hygiene).
2. **PRD-vs-code reconciliation** from a gap-analysis pass that filed `#713`–`#718`. Net of those: the PRD now matches the wire (wrapped envelope on list endpoints, `chi` removed from §6, deviations table refreshed); `auth_events` gained the missing `username` column (capped at 64 bytes against pasted-password leaks); `CHAT_LOG_LEVEL` wired through `log/slog` at bootstrap with info→stdout / warn+→stderr split, and a follow-up sweep migrated the `wiring/`/`wsapi/`/`http/` `log.Printf` callsites to slog with structured attrs (access-log middleware deliberately stays on stdlib `log` to keep AC-1's raw-stderr contract); WS now emits a typed `{type:"error",data:{code,message}}` frame before close on body-cap and rate-limit paths.

The headline outcome of the session is **`#678` (Option A): per-worktree runtime isolation for dispatched agents.** Subagents kept leaking Edit/Write into the parent checkout (1–3× per session) despite repeated RULE 0 prompt-side strengthening; the structural fix landed as PR #748 — a `.claude/agent-worktree-settings.template.json` template plus `.claude/scripts/write-agent-worktree-settings.sh` that the agent's §0 runs as its first tool call, materializing a per-worktree `settings.local.json` with `Edit`/`Write`/`MultiEdit` deny rules pointing at the resolved parent absolute path plus `sandbox.enabled: true`. This is the two-layer model the 2026 industry brief recommends (worktrees for source-state isolation, OS-level sandbox for runtime/FS isolation). The original Option D (switch dispatches from `Agent({isolation: "worktree"})` to a chrooting `EnterWorktree` tool) was investigated and ruled out — `EnterWorktree`'s implementation is `process.chdir(worktreePath)`, not a jail, and a `validateInput` guard refuses calls from agents already running with `isolation: "worktree"`. Findings logged on `#678`.

Token-efficiency cleanup also landed: idle-banner files deleted from both skills, dispatch-template `RULE 0` block deduped (the rule survives in the agent definitions, which each agent loads on first tool call), `phase-loop` and `pr-review-loop` SKILL.md repeats trimmed, and the `-C $WORKTREE` harness recipe documented in both agent definitions (verified working for `git`, `go`, `pnpm`; bare invocations have been observed killed mid-call). Net 8 files / +50 / −214 lines for the supervisor-warm-path changes.

PR count this session: ~25 merged. The `pr-reviewer` agent merged everything end-to-end without escalation except for PR #724 (twice-killed mid-review on `rtk go build` denial; on the third dispatch it landed `rtk go -C "$WORKTREE" build ./...` and merged cleanly — the recipe is now in the agent definitions).

## What landed by area

### `#678` Option A — per-worktree runtime isolation (the structural win)

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #678 | #748 | Per-worktree `settings.local.json` deny rules + sandbox; agent §0 runs the script as first tool call; `.gitignore` for the generated file |
| #752 | #755 | Substitution sentinel renamed `PARENT_ABS` → `__PARENT_ABS__` (rarer string, can't collide with template prose) |
| #756 | #760 | `.claude/scripts/README.md` sentinel-name drift caught + fixed |
| #751 | #759 | Deny list expanded by 9 entries (`tsconfig.json`, `.golangci.yml`, `.prettierrc.json`, `.prettierignore`, `eslint.config.js`, `.gitignore`, `README.md`, `migrations/`, `diary/`) — total 55 deny rules |
| #753 (+ #762) | #763 | `MultiEdit` deny coverage made symmetric across all single-file targets; one paragraph in scripts README documents the defense-in-depth rationale; total 69 rules (23 each of Edit/Write/MultiEdit) |

Caveat observed: the `#403` worker (dispatched after Option A merged) still hit a parent leak on its first Edit/Write round and self-recovered manually. This means either (a) the `write-agent-worktree-settings.sh` script didn't fire for that dispatch path, (b) the rendered `settings.local.json` isn't being honored by the harness in the version used, or (c) the substitution produced an unexpected absolute path. Worth a fresh investigation if the next session sees the same class.

### PRD reconciliation (from `#713`–`#718` gap analysis)

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #713 | #726 | PRD §10 list-endpoint shape documented as `{channels:[…]}` / `{messages:[…]}` (matches wire); deviations row added |
| #715 | #725 | `CHAT_LOG_LEVEL` wired through `log/slog` at bootstrap; access-log middleware kept on stdlib `log` for AC-1 |
| #716 | #723 | `auth_events.username` column + plumbing through `LogAuthEvent`; existing tests still green |
| #717 | #724 | WS typed error frame before close (body-cap + rate-limit paths); unit + e2e drain the frame; `wsproto.SendRateLimitCloseReason` constant follow-up |
| #722 | (subsumed) | `#726` already bumped the §10 deviations intro count — issue closed |

### Code-review follow-ups

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #559 | #743 | Playwright regression-guard for messages-list `role="log"` + implicit `aria-live="polite"` (manual VO/NVDA/JAWS verification dropped as `not planned`) |
| #654 | #727 | `IS_AT_BOTTOM_TOLERANCE_PX` constant + tests derive boundary scrollTop from the constant |
| #697 | #711 | Tightened AC-1 changelog regex + `ONE_DAY_MS` constant |
| #704 (+ #706) | #709 + #749 | CHANGELOG.md phase-grouping (`**Phase 0**` form so AC-2 regex matches), AC-2 unskipped, AC-3 narrowing fix |
| #729 | #736 | Logout + WSTicket use `auth.UsernameFromContext` instead of redundant `LookupUserByID` |
| #730 | #739 | `auth_events.username` capped at 64 bytes inside `LogAuthEvent` to bound pasted-password footprint |
| #728 | #732 | `auth_handlers` register_failed comment reconciled with the trimmed-value behavior |
| #734 | #761 | Stdlib `log.Printf` sweep across `wiring/`/`wsapi/`/`http/` migrated to slog with structured attrs |
| #740 | #746 | `wsproto.SendRateLimitCloseReason` constant; both arms now share one source |
| #741 | #754 | `wsapi.ErrCode` named type for `writeErrorFrame`'s `code` parameter |
| #742 | #757 | 3-line PRD §10/SEC-8 comment dropped from `wsapi/handler.go` (`writeErrorFrame` docstring is the durable reference) |
| #744 | #747 | `aria-log.spec.ts` imports `TOKEN_KEY` from `apps/web/src/api.ts` (no more string drift) |
| #745 | #750 | `waitForChatShell(page, username)` helper extracted into `tests/e2e/playwright/helpers.ts`; `aria-log.spec.ts` + `loginInBrowser` use it |
| #735 | #737 | Explicit `return` after AC-3 changelog `expect.fail` for TS narrowing |
| #733 | #738 | slog level-split: info→stdout, warn+→stderr (small `splitHandler` over two `slog.NewTextHandler` instances) |
| #403 | (closed) | `TestAC3_Me_RequiresValidBearer` `/me with tampered signature` flake — already fixed in PR #500 (deterministic splice), no code change needed; closed with timeline note |

### Skill / agent token cleanup

PR landed directly to `main` (with explicit per-edit approval): trimmed both SKILL.md files, deleted both `idle-banner.md` references, deduped the RULE 0 block from both dispatch templates, compressed `§5b`/`§7` defensive prose in both agent definitions, dropped the `feedback_subagent_path_leakage.md` index line from `MEMORY.md` (the rule survives in the agent definitions), and added the `-C $WORKTREE` harness recipe to both agent §0 bodies. Net `−164` lines. Estimated savings ~14k tok/hr on busy sessions.

De-scoped (closed `not planned`):
- `#536` (diff-check before snapshotting username directory in `usePresence`) — issue body itself says "tackle together with whichever PR introduces the periodic reseed"; that PR doesn't exist yet.
- `#383` (internal-leak needle tightening) — issue's own `## Out of scope` section says "pre-emptive change today"; the trigger condition (handler that echoes user-supplied path fragments) hasn't shipped.

## What did not land

Nothing material. Every actionable Phase 4 sub-issue closed; every `not planned` close had explicit issue-body justification.

## Standing concerns for the next session

1. **Option A enforcement validation.** The `#403` worker's leak after Option A merged means runtime enforcement isn't yet provably in place. Worth a deliberate adversarial test: dispatch a worker with a prompt that asks it to write to a parent-rooted path and confirm the deny rule actually fires.
2. **Worktree clutter.** `git worktree list` shows ~50 stale `.claude/worktrees/agent-*` entries from earlier sessions (many marked `(detached HEAD) locked`). They don't affect correctness but mask new agents in the listing. A `/cleanup-stale-worktrees` script would be useful when the next session starts.
3. **#704 regression guard.** PR #709's `**Phase 0 — Walking skeleton.**` form not matching AC-2's regex meant the test sat skipped for ~30 hours after the production-side change shipped. The lesson is: *if a PR lands the production half of a `Closes` linkage but defers the test half, the issue has to stay open AND the deferred-test work needs to actually run the test before declaring done*. The follow-up worker caught it on retry; the supervisor should have caught it on PR #709 review.
4. **Dispatch-template RULE 0 dedup.** The token-cleanup PR removed the duplicate RULE 0 from the dispatch templates. The rule survives in the agent definitions. If next session shows leak rates climb, consider re-adding the duplicate as a single-line reminder rather than the full block.

## Numbers

- Sub-issues opened in Phase 4: ~60 (incl. follow-ups filed during the session)
- Sub-issues closed in Phase 4: 60
- PRs merged in Phase 4: ~50 (including the post-noon burn between 12:00–17:00 UTC on 2026-05-05 where 47 of 54 closures landed)
- New `not planned` closures: 4 (`#536`, `#383`, `#722` subsumed by #726, `#758` duplicate of #756)
- Standing-rule-edit approvals: 1 (the supervisor's `chore(claude): trim skill+agent verbiage…` PR)
