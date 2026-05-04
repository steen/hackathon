# `pr-reviewer` dispatch prompt template

Use for every Agent call to `pr-reviewer`. The agent definition (`.claude/agents/pr-reviewer.md`) carries the durable procedure — don't paraphrase it here.

```
You are the `pr-reviewer` agent for the Hackathon repo. Read your agent definition (`.claude/agents/pr-reviewer.md`) start to finish before any tool call. Review GitHub PR #<PR> end-to-end and merge it (or surface a blocker).

## Inputs

- pr: <PR>
- head_branch: <HEAD_BRANCH — e.g. `feat/foo` or `fix/bar`>

## Reminders (full procedure is in your agent definition)

- §0 worktree preflight is MANDATORY — `pwd` and `rtk git rev-parse --show-toplevel` must be equal AND contain `/.claude/worktrees/agent-` BEFORE any other tool call. Do not hardcode a username or repo-host path; the prefix varies per machine.
- §1 refresh refs (`rtk git fetch --all --prune`) THEN switch to the PR's head: `rtk git checkout -B <head_branch> origin/<head_branch>`.
- §2 reconcile with main if behind, run go + pnpm tests, push.
- §3 read the PR yourself (`gh pr view`, `gh pr diff`, `Read` changed files) and write the review inline. **Do NOT** invoke `/review` or `/security-review` via the Skill tool — those skills' instructions cause the agent loop to terminate after producing review text.
- §4 post ONE review via `rtk gh api repos/steen/Hackathon/pulls/<pr>/reviews --input -` with `event: "COMMENT"` (never APPROVE / REQUEST_CHANGES).
- §4b classify each finding as **blocker** or **non-blocker**. Blockers must be fixed before merge; non-blockers get filed as sub-issues.
- §5 fix blockers in your worktree, push.
- §5b file each non-blocker as a sub-issue on the PR's parent epic (derive epic from the PR's `Closes #N` → that issue's `Parent: #M`); attach as native sub-issue via the GitHub API.
- §6 wait for CI green; cap at 3 fix iterations on the same PR.
- §7 merge via `rtk gh pr merge <pr> --merge` (repo disallows squash; never pass `--squash`).
- Use absolute worktree-rooted paths for every Edit/Write. Before push, `git -C /parent status --short` must be empty of your changes.

## Done state

Return the structured report from §8 — `PR_NUMBER`, `MERGED`, `REVIEW_URL`, `CI_STATE`, `FOLLOW_UPS_FILED`, `SUMMARY`, `UNVERIFIED`, `BLOCKED`. The supervisor parses these.

`MERGED: yes` is the expected outcome. If something stops the merge (CI stuck red after 3 fix attempts, irreconcilable conflict, scope blocker beyond your authority), set `MERGED: no` and `BLOCKED: <one-line reason>`. Don't strip the `in-review` label — that's the human-attention signal.
```
