---
name: phase-loop
description: One tick of the Hackathon repo's phased delivery loop. Picks the lowest-numbered open epic issue, scans its sub-issues for parallel-eligibility against the current open-PR conflict surface, and dispatches each eligible sub-issue to a fresh `issue-pr-worker` subagent in an isolated git worktree. Worker subagents mirror CI locally before pushing. Designed to run in two modes: `single-tick` (one pass, exit) and `auto-fire` (re-trigger this skill on every PR-merge event so newly-unblocked work is picked up immediately, plus a periodic safety wakeup). Idle behavior is explicit: when nothing is eligible, the skill exits cleanly without spinning.
---

# phase-loop

Supervises the Hackathon repo's phased issue-shipping pipeline. Driven by `/phase-loop` (single tick) or `/phase-loop auto` (continuous, re-fired on merge events).

## Runtime contract

- **Repo:** cwd is a git checkout of the Hackathon repo with `gh` authenticated for it. RTK is on PATH; every shell command goes through `rtk` per the global RTK rules.
- **Concurrency:** parallel work happens in subagents, each in its own git worktree at `.claude/worktrees/agent-<id>` via the Agent tool's `isolation: "worktree"`. The supervisor's primary working tree is never modified by workers.
- **Idempotency:** every step is safe to re-run. The skill picks at most one eligible foreground task and N parallel subagents per tick, then returns.
- **Authority:** the skill never merges PRs (user's job), never pushes to `main`, never edits source files itself — it only dispatches.

## Inputs

| Arg | Purpose |
|-----|---------|
| `auto` | Optional. When present, schedule a follow-up wakeup on every PR-merge event AND a safety net every 20 min. When absent, run one tick and exit. |
| `phase-override=<N>` | Optional. Force the skill to use epic `#<N>` instead of the lowest-numbered open epic. |

## Tick procedure

Run steps in order. **Bail at "exit silently" points** — emit no chat output beyond a one-line status the user can ignore.

### 1. Sync

```bash
rtk git fetch --all --prune
rtk git checkout main
rtk git pull --ff-only
```

If the working tree is dirty, **stop and report** — the user must clean up before the loop runs (otherwise step 8's worktree spawns inherit dirty state).

### 2. Inventory in-flight work

```bash
rtk gh pr list --state open --json number,title,headRefName,files
rtk git worktree list
```

For each open PR, capture the file paths it touches — that's the conflict surface for this tick. For each existing agent worktree, capture its branch and assume it's still running until `git worktree remove` succeeds in step 11.

### 3. Pick the phase

Find the lowest-numbered open issue with the `epic` label (or use `phase-override` if provided):

```bash
rtk gh issue list --state open --label epic --json number,title --limit 20
```

Read the chosen epic's body — its "Sub-issues" section lists what's in scope.

### 4. Filter sub-issues for parallel-eligibility

For each sub-issue, all of these must be true:

1. It is open and not already assigned to an in-flight PR (cross-reference step 2).
2. Its file footprint (read the `Spec:` link in the issue body, or guess narrowly from the title — never widen) does NOT overlap any open PR's file footprint.
3. It does NOT depend on code that only exists in an unmerged PR (e.g. an issue that wires an api-client method blocked on the api-client PR).
4. It does NOT require editing conflict-magnet files (`apps/server/main.go`, `CHANGELOG.md`) when another in-flight PR also touches that file.
5. Branching off `origin/main` will not require any cherry-pick from an open PR (no stacking).

If the filter produces an empty set, **idle**: emit a one-line idle marker (no work to dispatch) and skip to step 12.

### 5. Plan the batch

From the eligible set:

- **Foreground task:** the lowest-numbered eligible sub-issue. The supervisor itself does NOT implement it; both foreground and parallel work are dispatched to `issue-pr-worker` subagents. The "foreground" naming is historical — every task today is dispatched.
- **Parallel set:** any other eligible sub-issue whose footprint is disjoint from the foreground task AND from every other parallel pick AND from every in-flight PR. Cap parallelism at 3 subagents per tick to stay reviewable.

For each sub-issue you'll dispatch, derive:

- `branch_name`: `feat/<slug>` for features, `fix/<slug>` for bug/sec fixes
- `closes_or_refs`: `Closes` for ordinary issues, `Refs` if the issue is an umbrella tracking multiple findings
- `footprint`: the paths the worker may touch — be explicit, narrow, glob-friendly
- `spec_path`: the `specs/plans/...` file linked in the issue body (omit if absent)

### 6. Sec-fix dispatch (additional source)

If the open epic has a sub-issue like "Phase X — Security audit findings" (an umbrella), each remaining unfixed finding is also a candidate. Pick disjoint findings into the parallel set the same way as ordinary sub-issues, with `closes_or_refs: Refs`. Skip findings whose suggested fix is "info" severity unless the rest of the queue is empty.

### 7. Dispatch subagents

For every sub-issue in the planned batch (foreground + parallel), call the `issue-pr-worker` agent with `isolation: "worktree"` and a prompt that includes the inputs above. Always run subagents in the background (`run_in_background: true`). The subagent reads CLAUDE.md, mirrors CI locally, and gates its push on a green local mirror — see `references/worker-prompt-template.md` for the exact prompt scaffold.

Important: never dispatch two subagents whose footprints overlap, even partially. If you find yourself wanting to, queue one for the next tick instead.

### 8. Track dispatched work

Record each dispatched subagent in tasks (`TaskCreate`) with subject `<branch_name> subagent (#<issue>) opens PR + green CI`. Set status `pending`; the user-visible task list shows what's running.

### 9. Wait for completions

The supervisor exits and returns control. Subagent completion notifications arrive automatically as `task-notification` system reminders; the skill (when re-invoked) treats those as the trigger to run another tick. Do not poll subagent state from within this tick.

### 10. On any subagent completion

When a subagent reports `LOCAL_CI_MIRROR: green` and `CI_STATE: green`:

1. Verify the PR exists: `rtk gh pr view <num> --json state,mergeable`.
2. Mark its task `completed`.
3. Do NOT merge — that's the user's job.

If the subagent reports red (locally or after push), update the task with the failure detail and surface to the user. Do not auto-retry.

### 11. Cleanup

For each completed subagent's worktree:

```bash
rtk git worktree remove -f -f .claude/worktrees/agent-<id>
```

Locked worktrees need `-f -f` (one to override the lock, one to ignore the worktree's modifications). Skip cleanup for any worktree whose subagent is still running.

After a successful merge (yours or anyone's): `rtk git fetch --all --prune` so local tracking refs match remote, especially with auto-delete-on-merge.

### 12. Re-fire (if `auto` mode)

If invoked with `auto`:

- Schedule a periodic safety wakeup with `ScheduleWakeup` at 270 s with the prompt `/phase-loop auto`. (270 s is the largest delay that stays inside the 5-minute prompt cache TTL — picking 300 s would burn the cache without amortizing it. Re-read the ScheduleWakeup tool's cache guidance before deviating.)
- Whenever a PR-merge notification arrives between ticks, re-invoke `/phase-loop auto` immediately — don't wait for the safety wakeup. Merges shrink the conflict surface and may unblock previously-ineligible sub-issues.

If invoked without `auto`, exit cleanly. The user re-fires manually.

## Hard prohibitions

- The skill never merges PRs.
- The skill never edits source files itself — only dispatches via `issue-pr-worker`.
- The skill never spawns a subagent that overlaps another subagent's footprint, even by one file.
- The skill never picks the same sub-issue twice across simultaneous ticks (the in-flight PR check in step 2 catches this).
- The skill never falls through to the next epic until the current one has zero eligible sub-issues AND zero in-flight PRs targeting it.

## Sub-skills / references

- `references/worker-prompt-template.md` — the standard prompt scaffold passed to each `issue-pr-worker` dispatch.
- `references/eligibility-examples.md` — worked examples of conflict-surface analysis for tricky cases (PR touches `wsapi/handler.go`, sec finding wants to edit it; cli + web disjoint; etc.).
