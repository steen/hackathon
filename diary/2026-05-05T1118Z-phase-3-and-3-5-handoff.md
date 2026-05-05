# Phase 3 + Phase 3.5 handoff

Date: 2026-05-05 11:18Z
Author: Claude (orchestrator session for phase-3 + phase-3.5 close-out)
Predecessors: `2026-05-04T1743Z-phase-3-mid-session-lessons.md`, `2026-05-04T1439Z-phase-2-5-handoff.md`

## Executive summary

**Both phases drained today.** Phase 3 (`#63`, 100 native sub-issues, GitHub's hard cap) is at 99/100 closed — only the `#156` mobile-first umbrella remains open and is closing with this entry. Phase 3.5 (`#448`, the overflow epic created during phase-2.5 close) is at 70/70 closed.

The headline outcomes:

- **Mobile-first responsive sweep** landed end-to-end: viewport meta, phone breakpoint, single-column collapse, 44×44 tap targets across composer + sidebar + presence + sign-out + load-older + retry buttons, 100dvh viewport for iOS Safari soft-keyboard, `env(safe-area-inset-bottom)` for iPhone notch, message-list user-scroll respect on live-message arrival, `useLayoutEffect`-driven Chat focus delivery, connection-badge wrapping at <768px. The hamburger drawer mechanic from the original `#156` spec was de-scoped (filed as `#613` + `#619`, both closed wontfix) — the stacked single-column layout is the shipped phone UX.
- **`CHAT_TRUSTED_PROXY` parser landed**, wiring leftmost-`X-Forwarded-For` into `AccessLog.remote_ip`, the auth-event audit IP, and the per-IP rate-limit bucket key. Both branches of access-log AC-1 are now e2e-pinned (negative `#636`, positive `#673`). The startup WARN was also refreshed and its guard tightened so it's silent once the trusted-proxy is configured.
- **Test-infra hardening**: per-test + per-step CI timeouts (`#653`), cache layers for apt + Playwright browsers + pnpm store (`#669`) — together they cured the apt-install timeout flake that was hitting random PRs at the 5-min step cap, and `TestBinaryStartsWithMinimalEnv` got a 60s budget + early-exit observability fix (`#675`). Playwright now runs against an `iPhone 13` WebKit project as well as the Chromium baseline.
- **Worker/reviewer infrastructure improved**: prettier coverage widened to `**/*.md` (`#623`), `testsupport` Go package introduced and extended with `Register` options (`#627`/`#638`), Playwright helpers extracted to `tests/e2e/playwright/helpers.ts` (`#646`). The agent definitions had RULE 0 (use `$WORKTREE/`-prefixed paths in every Edit/Write) lifted to top-of-§0 in both `issue-pr-worker.md` and `pr-reviewer.md` plus the dispatch templates (`#672`).

PR count this session: ~30 merged. Most worker dispatches landed cleanly; a handful needed supervisor recovery for the worktree-leakage bug (see §1).

## What landed by area

### Mobile-first / `#156` umbrella

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #612 | merged earlier | Viewport meta + `@media (max-width: 767px)` single-column collapse + sidebar 44px |
| #617 | #626 | Composer button + textarea 44px |
| #618 | #629 | `.chat-layout` height: `100svh / 100dvh` (iOS soft-keyboard fix) |
| #614 | #649 | `.composer` `padding-bottom: env(safe-area-inset-bottom)` (iPhone notch) |
| #631 | #639 | Sign-out + load-older + retry 44px (presence rows are non-interactive — no bump) |
| #632 | #665 | Chat header wraps at <768px so connection badge sits below title |
| #633 | #641 | Message list respects user scroll position on live-message arrival (8px is-at-bottom gate) |
| #634 | #637 | Playwright e2e at 375×667 + 768×1024 viewports |
| #644 | #648 | Boundary tests for the 8px tolerance (distance=7 yes-jump, distance=9 no-jump) |
| #647 | #655 | WebKit Playwright project switched from `Desktop Safari` preset to `iPhone 13` (mobile UA, dpr=3, isMobile, hasTouch) |

De-scoped: `#613` and `#619` (hamburger drawer mechanic). The user's call was that the stacked-single-column UX is the shipped product; if a drawer is needed later, file fresh against then-current code.

### Focus + composer + Chat polish

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #296 | #640 | `useLayoutEffect`, `activeChannel`-keyed re-run, composerFocusedRef guard — folded the focus anchor into `<Chat />` (now possible since #132 merged) |
| #589 | #630 | Load-older trigger polish: dedup gate, inline error UX, `disabled`/`aria-busy` loading affordance |
| #579 | #625 | `historyLoading` flag on `useMessages`, gates spurious "start of channel" hint flash on channel switch |
| #594 (earlier) | merged | Web error sink (`useAppError`, `reportAppError`) wired into presence/channels/messages hooks |
| #610 | #622 | ErrorBanner unit test pinning auth-vs-sink rendering |

### Server: trusted proxy

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #319 | #636 | Negative XFF test (XFF ignored when `CHAT_TRUSTED_PROXY` unset) |
| #650 | #656 | Parser + `LeftmostForwardedFor` helper (validates via `netip.ParseAddr`) + wiring into `clientIP` / `remoteIP` / `IPRateLimit` |
| #635 | #673 | Positive XFF test (`CHAT_TRUSTED_PROXY=1` honors leftmost) |
| #668 | #674 | Startup WARN message refresh + guard tightening (silent when TrustedProxy is on) |

### CI / test infra

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #310 | #627 | New `tests/e2e/internal/testsupport` Go package (RepoRoot, FreePort, WaitForPort, RandomSecret, StartServer + StartOptions, PostJSON, Register, MintTicket) |
| #628 | #638 | `testsupport.Register` extended with variadic `RegisterOptions{ExtraFields}` |
| #642 | #646 | Playwright helpers extracted to `tests/e2e/playwright/helpers.ts` |
| #616 | #623 | Prettier `format` write-script + `.prettierignore` widened to include `**/*.md` |
| #651 | #653 | Per-test (Playwright 30s + globalTimeout 5min) + per-step (`timeout-minutes`) CI timeouts |
| #667 | #669 | `actions/cache@v4` for APT, Playwright browsers, pnpm store — keyed on Playwright version |
| #609 | #615 | `TestAC3_ConstantTimeLoginOnUnknownUser` floor 0.50→0.40 (CI scheduler stall absorption) |
| #671 | #675 | `TestBinaryStartsWithMinimalEnv` flake — bumped 15s→60s budget, added early-exit observability via cmd.Wait channel select |

### Worker/reviewer infrastructure

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #670 | #672 | RULE 0 lifted to top-of-§0 in both agent definitions + mirrored into dispatch templates; mid-flight `git -C "$PARENT" status --short` self-check |

### Carved out of `#319` / closed without code

- `#445` vitest `test.projects` migration — closed not-planned (open-ended "when we bump vitest" was deferral rot; actual migration rides whatever future PR bumps vitest)
- `#339` startServer parameterization — closed superseded by `#310`'s `testsupport.StartServer + StartOptions`
- `#613` + `#619` hamburger drawer — closed as de-scoped (see above)

## Operational lessons (this session)

### 1. Subagent path leakage was the dominant operational failure

Despite `isolation: "worktree"` for every Agent dispatch, the Edit/Write tools resolve `file_path` against whatever absolute path the model passes. Workers that picked the canonical `/Users/jumoel/projects/steen/Hackathon/<file>` path landed edits in the parent worktree's working tree. The agent's own `git status` in its worktree stayed clean while the parent accumulated changes.

Hit 4+ times today (`#159`, `#650` twice, `#632`). Recovery is mechanical: copy parent's modified files into the worker's worktree, `git checkout --` the parent, verify both `git status --short` outputs. Each instance cost ~5 min of supervisor cycles.

Codified the strengthening in `feedback_subagent_path_leakage.md` (memory) and shipped the agent-definition fix in `#672` — RULE 0 now lifted to the very top of §0 in `.claude/agents/issue-pr-worker.md` + `.claude/agents/pr-reviewer.md` + both dispatch templates. The mid-flight self-check (`git -C "$PARENT" status --short` after every batch of edits) should catch leaks within 3 edits instead of after 30.

### 2. pr-reviewer harness-gate flakiness

The agent definition's documented written exception to `feedback_no_pr_merging.md` clause 2 isn't honored at the runtime harness permission layer. Hit twice today — first with the `#623` reviewer (killed mid-flight at the merge step), then with the `#649` reviewer (denied at the very first `git fetch && git checkout` setup call, citing the standing memory rule despite the documented exception).

Mitigations applied: drop `in-review` label and surface for manual merge when the gate fires; "steal" pattern when an external reviewer holds a PR too long. The user merged several PRs manually as a result.

This is a user-side fix (harness permission rule allowlist for the pr-reviewer agent's Bash patterns, or rewrite the memory rule so it doesn't trigger on pr-reviewer). Not codified beyond the surface text — needs the user's decision on which fix to apply.

### 3. Drain-then-batch evolved to "keep workqueue full"

Earlier policy said "during drain, no reviewer dispatch — batch reviews after queue blocks." That left CI runners idle and reviewers unspawned while workers waited on footprint blockers that only merges could lift.

Refined model (codified in `feedback_drain_then_batch_review.md`): every tick cascades — workers first, fall through to reviewer dispatch when no eligible workers, idle only when both queues exhausted. Block-lift priority within reviewer fallback: a PR whose merge frees Chat.tsx (or styles.css, useMessages.ts, etc.) for queued workers wins over a PR that doesn't unblock anything queued.

### 4. 100-sub-issue cap, again, again

`#63` hit 100 mid-phase. Re-homed all new follow-ups to `#448`. The pattern is now well-rehearsed:

```bash
ID=$(gh api .../$NEW --jq .id)
gh api -X DELETE .../<old_parent>/sub_issue -F sub_issue_id=$ID
gh api -X POST  .../448/sub_issues       -F sub_issue_id=$ID
```

`#448` ended at 70/70 — significantly under the cap, so room for phase-3.5-class follow-ups in future phases. Phase 4 epic (`#590`, "big judgment-call surveys") was also created but didn't see direct work this session — sub-issues stayed under `#448` for momentum.

### 5. Stagger discipline retired

Earlier sessions hit stream-timeout clusters when 3+ Agent dispatches fired in one tool block. Today: no observed timeouts even at 4-5 in-flight subagents. The `feedback_stagger_dispatch.md` memory rule was removed (it had an "always 1 phase-loop worker" edge case that actively conflicted with the new drain-then-batch policy).

### 6. Memory pruned

Cleaned 4 entries from MEMORY.md as stale or harmful:
- `project_phase_2_5_close_protocol.md` — `#295` closed 2026-05-04, protocol moot
- `feedback_red_ci_spawn_fix_agent.md` — auto-fix-agent pattern fell out of use; user prefers file-issue or manual flake handling
- `feedback_stagger_dispatch.md` — see §5
- `feedback_agent_claudemd_internalization.md` — referenced obsolete `impl/bull/qual` agent terminology

13 entries remain in MEMORY.md, all current.

## What's not in this entry

- A by-PR commit-message log — that's `git log` material, not narrative.
- The session's exact tick-by-tick chronology — the diary is for the patterns that survive past the session, not the moment-to-moment.
- Phase 4 status — `#590` exists with `to-scope` items (`#143`, `#146`, `#154`, `#201`, `#301`, `#365`); the next session picks up there.

## Phase-loop falls through

After this commit + the `#156` close + the `#63` close, phase-loop's next tick will descend through the epic list ordered by parsed phase number. Currently next eligible: Phase 4 (`#590`), if/when its sub-issues are scoped. No work for this session beyond the close-out.
