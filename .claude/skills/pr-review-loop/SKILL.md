---
name: pr-review-loop
description: One tick of the PR review loop. Picks open PRs without `in-review`, dispatches a `pr-reviewer` subagent (in its own git worktree) per PR up to a parallel cap, then exits. Modes — `single-tick` (one pass, exit) and `auto` (re-fire on PR-merge events + safety wakeup at 270s). Idle ticks emit a banner and exit cleanly.
---

# pr-review-loop

Driven by `/pr-review-loop` (single tick) or `/pr-review-loop auto` (continuous).

## Runtime contract

- Cwd is the Hackathon repo, `gh` authenticated, `rtk` on PATH.
- Each PR review runs in a `pr-reviewer` subagent dispatched with `isolation: "worktree"` so the harness creates `.claude/worktrees/agent-<id>` per review. The supervisor's primary working tree is never modified.
- The `in-review` GitHub label is the per-PR lock — only one tick at a time holds it. Closing the PR (merge or abandon) auto-removes it.
- Every step is idempotent.
- The skill never edits source files itself — only dispatches.

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

Drop the supervisor's own user from review candidates if their author login is the same as `gh api user --jq .login` — agents shouldn't review their own work; let a human or a different agent do it. (Skip this filter if the team is single-user.)

### 3. Filter eligible PRs

A PR is eligible iff all are true:

1. `state == "open"` and not draft.
2. No `in-review` label (lock not held).
3. `mergeable` is `"MERGEABLE"` or `"UNKNOWN"` (skip `"CONFLICTING"` until the author resolves).
4. Not currently the head of any in-flight `pr-reviewer` subagent (cross-reference active task list).

If empty, **idle** — emit `references/idle-banner.md` (small, varied, mood-appropriate) and skip to step 9.

### 4. Plan the batch

Take the lowest-numbered eligible PR first. Add more whose head branches are distinct (worktrees are per-agent so file overlap doesn't matter; the constraint is "no two ticks on the SAME PR"). Cap at **2 parallel reviews per tick** to keep the review noise reviewable by a human.

### 5. Claim + dispatch

For each PR in the planned batch:

1. **Claim**: `rtk gh pr edit <pr> --add-label in-review`. This lock prevents another tick from picking the same PR.
2. **Dispatch** a `pr-reviewer` subagent with `isolation: "worktree"` and `run_in_background: true`. The prompt must include `pr` and `head_branch` inputs and reference the agent definition (`.claude/agents/pr-reviewer.md`) for the procedure. Use `references/reviewer-prompt-template.md` for the scaffold.
3. **Track** via `TaskCreate` — subject `pr-reviewer subagent (#<pr>) reviews + merges`, status `pending`.

If the dispatch itself fails (worktree creation error, etc.), drop the `in-review` label so the next tick retries: `rtk gh pr edit <pr> --remove-label in-review`.

### 6. Wait

Exit. Subagent completion notifications arrive as `task-notification` system reminders; the next `/pr-review-loop auto` tick processes them.

### 7. On completion

For each notification:

1. **`MERGED: yes`** — verify the PR is closed (`rtk gh pr view <pr> --json state,mergedAt`), mark task `completed`. The auto-removed `in-review` label confirms cleanup.
2. **`MERGED: no` with `BLOCKED:` set** — the PR needs human attention. Leave the `in-review` label as the signal, mark task `completed` with a note recording the blocker (CI red, can't reconcile, etc.). Surface to the user.
3. **Truncated report** — look up the PR directly: `rtk gh pr view <pr> --json state,mergedAt,reviewDecision`. If `MERGED`, treat as (1). If `OPEN` and a recent review exists (`rtk gh api repos/steen/Hackathon/pulls/<pr>/reviews --jq '.[-1].submitted_at'`), the agent did its job but didn't merge — surface and treat as (2).
4. **Stalled / no notification** — see step 8's failed-agent path.

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

- Never review your own PRs (filter by author in step 2).
- Never `event: "APPROVE"` / `"REQUEST_CHANGES"` on a posted review (the agent enforces this; the supervisor doesn't post reviews directly).
- Never dispatch two subagents against the same PR.
- Never strip the `in-review` label except (a) when claim+dispatch fails (step 5), or (b) when reclaiming a stalled worker after WIP recovery (step 8). Otherwise let the merge auto-remove it.
- Never edit source files in the supervisor — only dispatch.

## References

- `references/reviewer-prompt-template.md` — dispatch scaffold.
- `references/idle-banner.md` — pr-review-loop's own idle banner spec (smaller + different mood register from phase-loop's; see that file).
