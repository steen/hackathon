# `issue-pr-worker` dispatch prompt template

Use this scaffold for every Agent call to `issue-pr-worker`. Fill the bracketed slots from step 5 of the parent skill.

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

## Reminders

- §0 worktree preflight is MANDATORY before any other tool call. After capturing `$WORKTREE`/`$PARENT`, run `.claude/scripts/write-agent-worktree-settings.sh "$WORKTREE"` BEFORE any Edit/Write — this materializes per-worktree deny rules + sandbox (issue #678 Option A). RULE 0 prose remains as defense-in-depth.
- Stay strictly inside the footprint. Out-of-scope demands → stop and report (or file a follow-up per §8).
- Use `Closes #<ISSUE>` (or `Refs #<ISSUE>` per the input) on its own line in the PR body.
- The supervisor already added the `in-progress` label; don't touch it.

## Done state

Return the structured §9 report. If truncated, the supervisor will fall back to `gh pr view` — but still try to emit the full report.
```
