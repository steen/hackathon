---
name: pr-review-loop
description: One tick of the PR review loop — claim a candidate PR by adding the `in-review` label, run /review and /security-review, post line-level comments, fix issues, ensure CI green, and squash-merge. PRs already carrying the `in-review` label are skipped (claimed by another tick or held for human attention). Designed to be fired by `/loop 1m /pr-review-loop`, also runnable standalone.
---

# pr-review-loop

One tick of the PR review loop. Picks at most one candidate PR per invocation, drives it from "open" to "merged", then exits.

## Runtime contract

- **Repo:** the cwd is a git checkout with `gh` authenticated for the GitHub repo. The `in-review` label exists on the repo (created via `gh label create "in-review" --color fbca04`); if a future agent finds it missing it should re-create it.
- **Concurrency:** the `in-review` label is the lock. Only one tick at a time should hold it on a given PR.
- **Idempotency:** every step is safe to re-run on the same PR. Adding the label twice is a no-op; posting two reviews is verbose but not destructive; merging an already-merged PR is a `gh` no-op.

## Tick procedure

Run these steps in order. Bail at any "exit silently" point — emit no chat output.

### 1. Find the candidate PR

```bash
gh pr list --state open \
  --json number,title,headRefName,labels,mergeable,isDraft \
  --limit 20 \
  | jq '[.[] | select(.isDraft == false) | select(.labels | map(.name) | index("in-review") | not)]'
```

If the array is empty: emit a SMALL ASCII-art animal (≤10 lines) with a one-line idle thought reflecting frustration about the empty PR queue. Vary the animal and thought from tick to tick. Then exit silently.

If the array is non-empty: pick the lowest PR number. That's `$PR`.

### 2. Claim the PR with the `in-review` label

```bash
gh pr edit "$PR" --add-label in-review
```

This is the lock-acquire step. From this point, other ticks (or human reviewers running this skill) will not pick up `$PR` until the label is removed (which happens automatically when the PR closes).

### 3. Sync the working tree

```bash
git fetch origin "+$(gh pr view "$PR" --json headRefName -q .headRefName):refs/remotes/origin/$(gh pr view "$PR" --json headRefName -q .headRefName)"
git checkout "$(gh pr view "$PR" --json headRefName -q .headRefName)"
```

If the branch has diverged from `origin/main` (mergeable status `CONFLICTING` or `git compare main..HEAD` shows behind > 0), merge `main` in non-destructively:

```bash
git merge origin/main --no-ff -m "Merge main into $(git branch --show-current) — reconcile"
```

Resolve conflicts by:
- preferring `main`'s versions of files that have already been reviewed and merged in earlier PRs (CI workflow, test plans the team has already curated, server code that this PR re-introduces from a rolled-back ancestor);
- keeping the PR's net-new files and net-new diffs;
- merging `CHANGELOG.md` so the new entry sits above existing ones and carries the `(#$PR)` suffix.

Run `go build ./... && go test ./...` and `pnpm install --frozen-lockfile && pnpm -r --if-present test` locally. Push the merge.

### 4. Run /review and /security-review

Use the existing `/review` and `/security-review` slash commands. Both produce text findings; consolidate them yourself.

### 5. Post line-level review comments

Build a single review payload using GitHub's "create a review" API:

```bash
gh api repos/:owner/:repo/pulls/$PR/reviews --input - <<'JSON'
{
  "event": "COMMENT",
  "body": "<consolidated overview, security verdict, state notes>",
  "comments": [
    {"path": "...", "line": N, "side": "RIGHT", "body": "<concrete finding>"},
    ...
  ]
}
JSON
```

Constraints:
- `side: "RIGHT"` for added/modified lines;
- the `line` must be in the PR's diff (otherwise GitHub returns 422 "Line could not be resolved");
- if a finding doesn't anchor to a diff line (whole-PR concerns, missing files, etc.), put it in the `body` instead, prefixed with "**General concern (not on a diff line):**";
- leave the `event` as `"COMMENT"` so comments land unresolved — never `"APPROVE"` or `"REQUEST_CHANGES"`.

### 6. Fix the issues

Apply the minimum changes needed to address each comment. Re-run `go test` / `pnpm test`. Commit with a `fix(<scope>): ...` message that names the comment authors' findings and pushes back to the PR branch.

If the PR is "library only" (no entry-point change in `apps/server/main.go`/`apps/cli/main.go`) and the reviewer raised a future-facing concern (e.g. "this will break /ws when wired in"), still fix it now — that's cheaper than re-opening the issue later.

### 7. Wait for CI to go green

```bash
until [ "$(gh pr view "$PR" --json mergeStateStatus -q '.mergeStateStatus')" = "CLEAN" ] && \
      [ "$(gh pr view "$PR" --json statusCheckRollup -q '[.statusCheckRollup[] | select(.status != "COMPLETED")] | length')" = "0" ] && \
      [ "$(gh pr view "$PR" --json statusCheckRollup -q '[.statusCheckRollup[] | select(.conclusion != "SUCCESS")] | length')" = "0" ]; do
  sleep 12
done
```

If CI fails: fix the failure (read the run log via `gh run view --log-failed`), push, repeat. Cap at 3 fix iterations — if the PR is still red after the third, leave a final comment summarizing the blocker and exit. Don't strip the `in-review` label — it signals "human, take a look."

### 8. Squash-merge

```bash
gh pr merge "$PR" --squash \
  --subject "<title> (#$PR)" \
  --body "<one-paragraph summary of net-effect changes>"
```

The merge automatically closes the PR, which removes the `in-review` label.

### 9. Sync local main

```bash
git checkout main
git fetch origin main
git reset --hard origin/main
```

Exit. The next tick will pick up the next candidate.

## CHANGELOG policy

The CHANGELOG uses timestamped sections, newest-first, with the format `## YYYY-MM-DD HH:MMZ — <one-line summary> (#<PR>)` placed under `## Planned (next)`. Inside, use `Added` / `Changed` / `Fixed` / `Removed` subheadings with one to three high-level bullets — never a commit-by-commit log.

**Skip the CHANGELOG entry** when the PR is purely internal tooling/CI/docs with no user-visible or operational impact. Note that decision in the merge body. The current bar:

| PR kind | CHANGELOG? |
|---------|-----------|
| Production code (server, CLI, web app) | yes |
| Operational behavior (CI, CHANGELOG itself, deployment scripts) when it affects how the project ships | yes |
| Test scaffolding, agent config, plan/spec docs, `.archon/` / `.claude/` tooling | no |

## Notes

- This skill is the operational counterpart to the `loop` skill: `loop` schedules; `pr-review-loop` is what gets scheduled.
- It deliberately leaves comments **unresolved** so the human PR owner sees them on next visit. Resolution is human-driven.
- Keep ASCII animals small (≤10 lines) when the queue is empty so the output fits in a notification card.
