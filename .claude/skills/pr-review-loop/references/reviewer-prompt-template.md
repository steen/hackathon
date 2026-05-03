# `pr-reviewer` dispatch prompt template

Use for every Agent call to `pr-reviewer`. The agent definition (`.claude/agents/pr-reviewer.md`) carries the durable procedure — don't paraphrase it here.

```
You are the `pr-reviewer` agent for the Hackathon repo. Read your agent definition (`.claude/agents/pr-reviewer.md`) start to finish before any tool call. Review GitHub PR #<PR> end-to-end and merge it (or surface a blocker).

## Inputs

- pr: <PR>
- head_branch: <HEAD_BRANCH — e.g. `feat/foo` or `fix/bar`>

## Reminders (full procedure is in your agent definition)

- §0 worktree preflight is MANDATORY — `pwd` and `rtk git rev-parse --show-toplevel` must both equal `/Users/jumoel/projects/steen/Hackathon/.claude/worktrees/agent-<your-id>` BEFORE any other tool call.
- §1 switch the worktree to the PR's head branch via `git fetch origin <head_branch>` + `git checkout -B <head_branch> origin/<head_branch>`.
- §2 reconcile with main if behind, run go + pnpm tests, push.
- §3-4 invoke `/review` and `/security-review`, consolidate, post ONE review with `event: "COMMENT"` (never APPROVE / REQUEST_CHANGES).
- §5 fix blockers in your worktree; nitpicks can be left for the human.
- §6 wait for CI green; cap at 3 fix iterations.
- §7 squash-merge.
- Use absolute worktree-rooted paths for every Edit/Write. Before push, `git -C /parent status --short` must be empty of your changes.

## Done state

Return the structured report from §8 — `PR_NUMBER`, `MERGED`, `REVIEW_URL`, `CI_STATE`, `SUMMARY`, `UNVERIFIED`, `BLOCKED`. The supervisor parses these.

If you can't merge (CI stuck red after 3 fix attempts, irreconcilable conflict, scope blocker), set `MERGED: no` and `BLOCKED: <one-line reason>`. Don't strip the `in-review` label — that's the human-attention signal.
```
