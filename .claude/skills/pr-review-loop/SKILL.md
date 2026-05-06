---
name: pr-review-loop
description: One tick of the PR review loop. Picks open PRs without `in-review`, dispatches a `pr-reviewer` subagent (in its own git worktree) per PR up to a parallel cap, then exits. Modes — `single-tick` (one pass, exit) and `auto` (re-fire on PR-merge events + safety wakeup at 270s).
---

# pr-review-loop

Driven by `/pr-review-loop` (single tick) or `/pr-review-loop auto` (continuous).

## Runtime contract

- Cwd is the Hackathon repo, `gh` authenticated, `rtk` on PATH.
- Each PR review runs in a `pr-reviewer` subagent dispatched with `isolation: "worktree"` so the harness creates `.claude/worktrees/agent-<id>` per review. The supervisor's primary working tree is never modified.
- The `in-review` GitHub label is the per-PR lock — only one tick at a time holds it. Closing the PR (merge or abandon) auto-removes it.
- Every step is idempotent.
- The skill never edits source files itself — only dispatches.
- Per-worktree runtime isolation: the reviewer's §0 first-action runs `.claude/scripts/write-agent-worktree-settings.sh "$WORKTREE"`, which materializes `<worktree>/.claude/settings.local.json` with deny rules against the parent repo's editable code surface plus `sandbox.enabled: true`. This layers a harness-level reject on top of RULE 0's prompt-side rule (issue #678). The dispatched reviewer runs the script — the supervisor's only job is to keep the §0 reminder in the dispatch prompt.

## Inputs

| Arg | Purpose |
|-----|---------|
| `auto` | Schedule a follow-up wakeup on PR-merge events + safety net every ~5 min. Without it, run one tick and exit. |

## Tick procedure

Bail at "exit silently" points — emit no chat output beyond a one-line status the user can ignore.

### 1. Sync

```bash
rtk git fetch --all --prune
rtk git checkout main
rtk git pull --ff-only
```

Verify the `in-review` label exists; if missing, recreate: `rtk gh label create "in-review" --color fbca04`. If working tree is dirty, **stop and report**.

### 2. Inventory

```bash
rtk gh pr list --state open --json number,title,headRefName,labels,mergeable,isDraft,author --limit 30
rtk git worktree list
```

### 3. Filter eligible PRs

A PR is eligible iff all are true:

1. `state == "open"` and not draft.
2. **No `in-review` label** — STRICT. The label is a cross-process lock that another reviewer may hold. **Never strip an `in-review` label you didn't set in this same tick.** If the label is on, assume someone else is reviewing it; skip and move on.
3. `mergeable` is `"MERGEABLE"` or `"UNKNOWN"` (skip `"CONFLICTING"` until the author resolves).

If empty, **idle** — skip to step 9.

If you suspect a PR's `in-review` label is stale (no activity in >24h, or you can prove a sibling tick set it but crashed), surface to the user with the evidence — do not strip it autonomously.

### 4. Plan the batch

Take the lowest-numbered eligible PR first. Add more whose head branches are distinct (worktrees are per-agent so file overlap doesn't matter; the constraint is "no two ticks on the SAME PR"). Cap at **2 parallel reviews per tick** to keep the review noise reviewable by a human.

### 5. Claim + dispatch

For each PR in the planned batch:

1. **Claim**: `rtk gh pr edit <pr> --add-label in-review`. This lock prevents another tick from picking the same PR.
2. **Dispatch** a `pr-reviewer` subagent with `isolation: "worktree"` and `run_in_background: true`. The prompt must include `pr` and `head_branch` inputs and reference the agent definition (`.claude/agents/pr-reviewer.md`) for the procedure. Use `references/reviewer-prompt-template.md` for the scaffold; it already embeds the §0 reminder to run `.claude/scripts/write-agent-worktree-settings.sh "$WORKTREE"` as the first tool call after the path capture.
3. **Track** via `TaskCreate` — subject `pr-reviewer subagent (#<pr>) reviews + merges`, status `pending`.

If the dispatch itself fails (worktree creation error, etc.), drop the `in-review` label so the next tick retries: `rtk gh pr edit <pr> --remove-label in-review`.

### 6. Wait

Exit. Subagent completion notifications arrive as `task-notification` system reminders; the next `/pr-review-loop auto` tick processes them.

### 7. On completion

The agent's contract is "review posted, blockers fixed, non-blockers filed as sub-issues, CI green, merged" — `MERGED: yes` is the expected outcome.

For each notification:

1. **`MERGED: yes`** — verify the PR is closed (`rtk gh pr view <pr> --json state,mergedAt`). Mark task `completed`. The auto-removed `in-review` label confirms cleanup. `rtk git fetch --all --prune` to refresh local refs.
2. **`MERGED: no` with `BLOCKED:` set** — the PR needs human attention (CI stuck red after 3 fix attempts, irreconcilable conflict, scope blocker beyond the agent's authority). Leave the `in-review` label as the signal, mark task `completed` with the blocker noted, surface to the user.
3. **Truncated report** — look up the PR directly: `rtk gh pr view <pr> --json state,mergedAt`. If `mergedAt` is non-null, treat as (1). If the PR is still open, check `rtk gh api repos/steen/Hackathon/pulls/<pr>/reviews --jq '.[-1].submitted_at'`; if a recent review exists, the agent partially completed — surface as (2).
4. **Stalled / no notification** — see step 8's failed-agent path.

If the agent reports `FOLLOW_UPS_FILED: <numbers>`, those are sub-issues spawned on the parent epic; the next phase-loop tick will see them and queue them.

Worktree cleanup for the subagent's `.claude/worktrees/agent-<id>` is automatic when the harness's `isolation: "worktree"` task completes. If the worktree is still locked after a successful merge, run `rtk git worktree remove -f -f .claude/worktrees/agent-<id>` to reclaim it.

### 8. Cleanup for failed reviews

If a subagent stalls or returns without `MERGED:` and without a clear `BLOCKED:`, its agent-worktree may have uncommitted WIP. Before forcing a worktree removal:

```bash
rtk git -C .claude/worktrees/agent-<id> status --short
rtk git -C .claude/worktrees/agent-<id> log --oneline origin/main..HEAD
```

If WIP exists, push it on the agent's branch as a draft so the work survives. Only then `git worktree remove -f -f`. Drop the `in-review` label so the next tick can retry the PR.

### 9. Re-fire (if `auto`)

`ScheduleWakeup` at 270s with prompt `/pr-review-loop auto` (stays inside the 5-min prompt-cache TTL). When a PR-merge notification arrives between ticks, re-invoke `/pr-review-loop auto` immediately — merges shrink the candidate queue and may free up dispatch slots.

Without `auto`, exit cleanly.

## Hard prohibitions

- Self-review (same author as supervisor) IS allowed — pr-reviewer is a separate agent, and same-machine PRs would otherwise stall waiting for an external reviewer that may never arrive. The double-review guard is the `in-review` label, not the author check.
- Never `event: "APPROVE"` / `"REQUEST_CHANGES"` on a posted review (the agent enforces this; the supervisor doesn't post reviews directly).
- Never dispatch two subagents against the same PR.
- Never strip the `in-review` label except (a) when claim+dispatch fails (step 5), or (b) when reclaiming a stalled worker after WIP recovery (step 8). Otherwise let the merge auto-remove it.
- Never edit source files in the supervisor — only dispatch.

## References

- `references/reviewer-prompt-template.md` — dispatch scaffold.
