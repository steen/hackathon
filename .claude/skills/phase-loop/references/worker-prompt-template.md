# `issue-pr-worker` dispatch prompt template

Use this scaffold for every Agent call to `issue-pr-worker`. Fill the bracketed slots from step 5 of the parent skill.

The worker's agent definition (`.claude/agents/issue-pr-worker.md`) carries the durable rules — worktree preflight (§0), cache hygiene (§5), follow-up filing (§8). Don't re-paraphrase those here unless this dispatch needs an exception.

```
You are the `issue-pr-worker` agent for the Hackathon repo. Read your agent definition (`.claude/agents/issue-pr-worker.md`) start to finish before any tool call. Implement GitHub issue #<ISSUE> and open ONE PR.

## Inputs

- issue: #<ISSUE>
- footprint: <FOOTPRINT — explicit list of paths/globs you may touch>
- branch: <BRANCH — `feat/<slug>` or `fix/<slug>`>
- closes_or_refs: <Closes | Refs>
- spec: <SPEC_PATH or "none">

## Special-case overrides (only if the supervisor sets them)

- <Any one-off override the parent tick decided to allow, e.g. "you may edit `apps/server/main.go` because no other open PR is touching it; verified at <timestamp>">

## Reminders (full procedure is in your agent definition)

- §0 worktree preflight is MANDATORY — `pwd` and `rtk git rev-parse --show-toplevel` must both equal `/Users/jumoel/projects/steen/Hackathon/.claude/worktrees/agent-<your-id>` BEFORE any other tool call.
- Use absolute worktree-rooted paths for every Edit/Write. Never relative paths.
- Mirror EVERY ci.yml block locally before pushing. Run `golangci-lint cache clean && go clean -testcache` first to clear any stale-sibling-worktree references.
- Stay strictly inside the footprint. Out-of-scope demands → stop and report (or file a follow-up per §8).
- Use `Closes #<ISSUE>` (or `Refs #<ISSUE>` per the input) on its own line in the PR body.

## Done state

Run §8 (file follow-up sub-issues for any defects you can't handle in this PR). Then return the structured §9 report. The supervisor parses `LOCAL_CI_MIRROR`, `CI_STATE`, `UNVERIFIED`, and `SKIPPED` fields. If the harness's monitor truncates your final message, the supervisor will fall back to verifying state via `gh pr view` — but still try to emit the full report so the per-field information isn't lost.
```
