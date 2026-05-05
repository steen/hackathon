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

## RULE 0 — every Edit / Write path starts with `$WORKTREE/`

**Every `Edit` and `Write` tool call MUST pass a `file_path` that starts with `$WORKTREE/`** (your agent worktree). Never a parent-rooted path like `/Users/<...>/Hackathon/<file>` — `isolation: "worktree"` does NOT chroot Edit/Write, so parent-rooted paths leak edits into the parent checkout and race every other agent. This is the most common operational failure in this project (observed 4+ times on 2026-05-05 alone). Mid-flight, after every ~5 Edit/Write calls, run `rtk git -C "$PARENT" status --short` and abort if non-empty. Full procedure in §0 of your agent definition.

## Reminders (full procedure is in your agent definition)

- §0 worktree preflight is MANDATORY — `pwd` and `rtk git rev-parse --show-toplevel` must be equal AND contain `/.claude/worktrees/agent-` BEFORE any other tool call. Do not hardcode a username or repo-host path; the prefix varies per machine.
- Use absolute worktree-rooted paths for every Edit/Write. Never relative paths. Never parent-rooted paths.
- Mirror EVERY ci.yml block locally before pushing. Run `golangci-lint cache clean && go clean -testcache` first to clear any stale-sibling-worktree references.
- Stay strictly inside the footprint. Out-of-scope demands → stop and report (or file a follow-up per §8).
- Use `Closes #<ISSUE>` (or `Refs #<ISSUE>` per the input) on its own line in the PR body.
- The supervisor has already added the `in-progress` label to your sub-issue. Don't touch it — the supervisor manages it (drops on PR open or on stale-pushback). It's a cross-process lock so other supervisors don't pick up the same issue.

## Done state

Run §8 (file follow-up sub-issues for any defects you can't handle in this PR). Then return the structured §9 report. The supervisor parses `LOCAL_CI_MIRROR`, `CI_STATE`, `UNVERIFIED`, and `SKIPPED` fields. If the harness's monitor truncates your final message, the supervisor will fall back to verifying state via `gh pr view` — but still try to emit the full report so the per-field information isn't lost.
```
