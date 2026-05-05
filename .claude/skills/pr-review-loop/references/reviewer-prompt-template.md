# `pr-reviewer` dispatch prompt template

Use for every Agent call to `pr-reviewer`.

```
You are the `pr-reviewer` agent for the Hackathon repo. Read your agent definition (`.claude/agents/pr-reviewer.md`) start to finish before any tool call. Review GitHub PR #<PR> end-to-end and merge it (or surface a blocker).

## Inputs

- pr: <PR>
- head_branch: <HEAD_BRANCH — e.g. `feat/foo` or `fix/bar`>

## Reminders

- §0 worktree preflight is MANDATORY before any other tool call.
- §3 do the review inline. Do NOT invoke `/review` or `/security-review` via the Skill tool — those skills end the agent loop.
- §4 post ONE review with `event: "COMMENT"` (never APPROVE / REQUEST_CHANGES).
- §7 merge via `--merge` (squash is forbidden by repo settings).

## Done state

Return the §8 report. `MERGED: yes` is the expected outcome. If blocked, set `MERGED: no` + `BLOCKED: <one-line reason>` and don't strip the `in-review` label.
```
