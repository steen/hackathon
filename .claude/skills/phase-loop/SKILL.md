---
name: phase-loop
description: One tick of the Hackathon repo's phased delivery loop. Picks the lowest-numbered open epic, scans its sub-issues for parallel-eligibility against the open-PR conflict surface, and dispatches eligible work to `issue-pr-worker` subagents in isolated worktrees. Modes — `single-tick` (one pass, exit) and `auto` (re-fire on PR-merge events + safety wakeup). Idle ticks emit a banner and exit cleanly.
---

# phase-loop

Driven by `/phase-loop` (single tick) or `/phase-loop auto` (continuous).

## Runtime contract

- Cwd is the Hackathon repo, `gh` authenticated, `rtk` on PATH.
- Parallel work runs in `.claude/worktrees/agent-<id>` via the Agent tool's `isolation: "worktree"`.
- Every step is idempotent.
- The skill never merges PRs, pushes to main, or edits source files — only dispatches.

## Inputs

| Arg | Purpose |
|-----|---------|
| `auto` | Schedule a follow-up wakeup on PR-merge events + safety net every ~5 min. Without it, run one tick and exit. |
| `phase-override=<N>` | Force epic `#<N>` instead of lowest-numbered. |

## Tick procedure

Bail at "exit silently" points — emit no chat output beyond a one-line status the user can ignore.

### 1. Sync

```bash
rtk git fetch --all --prune
rtk git checkout main
rtk git pull --ff-only
```

If working tree is dirty, **stop and report** — the user must clean up.

### 2. Inventory

```bash
rtk gh pr list --state open --json number,title,headRefName,files
rtk git worktree list
```

Capture each open PR's file footprint (= conflict surface for this tick). Each agent worktree is "still running" until `git worktree remove` succeeds in step 11.

### 3. Pick the phase

```bash
rtk gh issue list --state open --label epic --json number,title --limit 20
```

Take the lowest-numbered open epic (or `phase-override`). Read its body.

### 4. Filter sub-issues for eligibility

A sub-issue is eligible iff all are true:

1. Open and not assigned to an in-flight PR.
2. Footprint disjoint from every open PR's footprint.
3. Doesn't depend on code that lives only in an unmerged PR.
4. Doesn't need to edit a conflict-magnet file (`apps/server/main.go`, `CHANGELOG.md`) that another open PR also touches.
5. Branchable off `origin/main` without cherry-picks (no stacking).

If empty, **idle** — emit `references/idle-banner.md` and skip to step 12.

### 5. Plan the batch

Lowest-numbered eligible sub-issue first; add more whose footprints are disjoint from the first AND from each other AND from every open PR. Cap at 3 subagents per tick.

For each, derive: `branch_name` (`feat/<slug>` or `fix/<slug>`), `closes_or_refs` (`Closes` or `Refs` for umbrella), `footprint` (explicit narrow paths), `spec_path` (from issue body, if any).

### 6. Sec-fix dispatch

If the epic has a "Security audit findings" umbrella, each remaining unfixed finding is a candidate (use `Refs`). Skip info-severity findings unless the rest of the queue is empty.

### 7. Dispatch

Call `issue-pr-worker` for every planned item with `isolation: "worktree"` + `run_in_background: true`. Use `references/worker-prompt-template.md` for the prompt scaffold.

Never dispatch two subagents with overlapping footprints. Queue the second for next tick if needed.

### 8. Track

`TaskCreate` per dispatch, subject `<branch_name> subagent (#<issue>) opens PR + green CI`, status `pending`.

### 9. Wait

Exit. Completion notifications arrive as `task-notification` system reminders; the next `/phase-loop auto` tick processes them. Do not poll.

### 10. On completion

The agent's §9 report can arrive truncated ("Waiting for the monitor."). For every notification:

1. **Full report, green+green** — `rtk gh pr view <num> --json state,mergeable` to verify, mark task `completed`. Do NOT merge.
2. **Truncated** — look up the branch directly: `rtk gh pr list --search "head:<branch>" --json number,state` then `rtk gh pr checks <num>`. If a PR exists and CI is green/pending, treat as green and proceed as (1).
3. **Red, stalled, or no PR** — see step 11's failed-agent path. Update task with failure detail, surface to user. Do not auto-retry.

### 11. Cleanup

**Pushed agents**: `rtk git worktree remove -f -f .claude/worktrees/agent-<id>` (`-f -f` overrides locked + dirty). Skip if subagent is still running.

**Failed agents** have uncommitted WIP that `worktree remove` destroys. First:

```bash
rtk git -C .claude/worktrees/agent-<id> status --short
rtk git -C .claude/worktrees/agent-<id> log --oneline origin/main..HEAD
```

Then either commit + push the WIP as a draft, leave the worktree in place for user inspection, or (only if explicitly disposable) remove. When in doubt, defer cleanup.

After any merge: `rtk git fetch --all --prune` so local tracking refs match.

### 12. Re-fire (if `auto`)

`ScheduleWakeup` at 270s with prompt `/phase-loop auto` (stays inside the 5-min prompt-cache TTL). If a PR-merge notification arrives between ticks, re-invoke `/phase-loop auto` immediately — merges shrink the conflict surface.

Without `auto`, exit cleanly.

## Hard prohibitions

- Never merge PRs.
- Never edit source files — only dispatch.
- Never spawn two subagents with overlapping footprints.
- Never pick the same sub-issue twice across simultaneous ticks.
- Never fall through to the next epic until the current has zero eligible sub-issues AND zero in-flight PRs.

## References

- `references/worker-prompt-template.md` — dispatch scaffold.
- `references/eligibility-examples.md` — conflict-surface examples.
- `references/idle-banner.md` — the idle banner spec.
