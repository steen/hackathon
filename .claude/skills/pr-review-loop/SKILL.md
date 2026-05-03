---
name: pr-review-loop
description: One tick of the PR review loop — claim a candidate PR by adding the `in-review` label, run /review and /security-review, post line-level comments, fix issues, ensure CI green, and squash-merge. Each tick runs in its own per-PR git worktree so multiple ticks can review different PRs in parallel without touching the user's primary working tree. PRs already carrying the `in-review` label are skipped (claimed by another tick or held for human attention). Designed to be fired by `/loop 1m /pr-review-loop`, also runnable standalone.
---

# pr-review-loop

One tick of the PR review loop. Picks at most one candidate PR per invocation, drives it from "open" to "merged", then exits.

## Runtime contract

- **Repo:** the cwd is a git checkout with `gh` authenticated for the GitHub repo. The `in-review` label exists on the repo (created via `gh label create "in-review" --color fbca04`); if a future agent finds it missing it should re-create it.
- **Concurrency:** the `in-review` GitHub label is the per-PR lock — only one tick at a time should hold it on a given PR. Per-tick filesystem isolation comes from a dedicated git worktree at `.claude/worktrees/pr-review/<PR>`, so multiple ticks reviewing *different* PRs can run in parallel without stepping on each other or on the user's primary working tree.
- **Idempotency:** every step is safe to re-run on the same PR. Adding the label twice is a no-op; posting two reviews is verbose but not destructive; merging an already-merged PR is a `gh` no-op; an existing worktree is reused after being reset to the PR's tip.

## Constants

- `REPO_ROOT`: the main working directory (cwd when this skill is invoked).
- `WORKTREE_BASE`: `$REPO_ROOT/.claude/worktrees/pr-review`
- `WORKTREE`: `$WORKTREE_BASE/$PR` (set once `$PR` is known in step 1).
- The user's primary working tree is never modified by this skill. After step 2, every `git` command runs via `git -C "$WORKTREE"`.

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

If the array is non-empty: pick the lowest PR number. That's `$PR`. Set `WORKTREE=$REPO_ROOT/.claude/worktrees/pr-review/$PR` and capture `HEAD_BRANCH=$(gh pr view "$PR" --json headRefName -q .headRefName)`.

### 2. Claim the PR with the `in-review` label

```bash
gh pr edit "$PR" --add-label in-review
```

This is the lock-acquire step. From this point, other ticks (or human reviewers running this skill) will not pick up `$PR` until the label is removed (which happens automatically when the PR closes).

### 3. Provision the per-PR worktree

```bash
mkdir -p "$WORKTREE_BASE"
git -C "$REPO_ROOT" fetch --quiet origin "+refs/heads/$HEAD_BRANCH:refs/remotes/origin/$HEAD_BRANCH" main
```

If `$WORKTREE` does not exist, create it on the PR's head branch:

```bash
git -C "$REPO_ROOT" worktree add -B "$HEAD_BRANCH" "$WORKTREE" "origin/$HEAD_BRANCH"
```

If `$WORKTREE` already exists (left over from a prior crashed tick), reuse it but normalize first:

```bash
git -C "$WORKTREE" checkout -B "$HEAD_BRANCH" "origin/$HEAD_BRANCH"
git -C "$WORKTREE" reset --hard "origin/$HEAD_BRANCH"
git -C "$WORKTREE" clean -fd
```

If `git worktree add` fails with "already checked out" — meaning the user has the PR branch open in the primary tree, or another tick is reviewing the same PR — drop the `in-review` label (`gh pr edit "$PR" --remove-label in-review`) and exit silently. Don't force.

From this point, every git command runs via `git -C "$WORKTREE"`. The user's primary tree is never touched.

### 4. Reconcile with main

If the branch has diverged from `origin/main` (mergeable status `CONFLICTING` or `git -C "$WORKTREE" rev-list --count HEAD..origin/main` > 0), merge `main` in non-destructively:

```bash
git -C "$WORKTREE" merge origin/main --no-ff -m "Merge main into $HEAD_BRANCH — reconcile"
```

Resolve conflicts by:
- preferring `main`'s versions of files that have already been reviewed and merged in earlier PRs (CI workflow, test plans the team has already curated, server code that this PR re-introduces from a rolled-back ancestor);
- keeping the PR's net-new files and net-new diffs;
- merging `CHANGELOG.md` so the new entry sits above existing ones and carries the `(#$PR)` suffix.

Run `(cd "$WORKTREE" && go build ./... && go test ./...)` and `(cd "$WORKTREE" && pnpm install --frozen-lockfile && pnpm -r --if-present test)`. Then `git -C "$WORKTREE" push origin "$HEAD_BRANCH"`.

### 5. Run /review and /security-review

Use the existing `/review` and `/security-review` slash commands against the worktree. Both produce text findings; consolidate them yourself.

### 6. Post line-level review comments

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

### 7. Fix the issues

Apply the minimum changes needed to address each comment inside `$WORKTREE`. Re-run `(cd "$WORKTREE" && go test ./...)` / `(cd "$WORKTREE" && pnpm test)`. Commit and push from the worktree:

```bash
git -C "$WORKTREE" add -A
git -C "$WORKTREE" commit -m "fix(<scope>): address review comments on #$PR"
git -C "$WORKTREE" push origin "$HEAD_BRANCH"
```

If the PR is "library only" (no entry-point change in `apps/server/main.go`/`apps/cli/main.go`) and the reviewer raised a future-facing concern (e.g. "this will break /ws when wired in"), still fix it now — that's cheaper than re-opening the issue later.

### 8. Wait for CI to go green

```bash
until [ "$(gh pr view "$PR" --json mergeStateStatus -q '.mergeStateStatus')" = "CLEAN" ] && \
      [ "$(gh pr view "$PR" --json statusCheckRollup -q '[.statusCheckRollup[] | select(.status != "COMPLETED")] | length')" = "0" ] && \
      [ "$(gh pr view "$PR" --json statusCheckRollup -q '[.statusCheckRollup[] | select(.conclusion != "SUCCESS")] | length')" = "0" ]; do
  sleep 12
done
```

If CI fails: fix the failure inside `$WORKTREE` (read the run log via `gh run view --log-failed`), push, repeat. Cap at 3 fix iterations — if the PR is still red after the third, leave a final comment summarizing the blocker and proceed to step 10 (worktree teardown) without merging. Don't strip the `in-review` label — it signals "human, take a look."

### 9. Squash-merge

```bash
gh pr merge "$PR" --squash \
  --subject "<title> (#$PR)" \
  --body "<one-paragraph summary of net-effect changes>"
```

The merge automatically closes the PR, which removes the `in-review` label.

### 10. Tear down the worktree

Whether the merge succeeded or the PR was abandoned in step 8, clean up the per-PR worktree so disk doesn't accumulate stale checkouts and the next tick on the same PR starts fresh:

```bash
git -C "$REPO_ROOT" worktree remove --force "$WORKTREE"
git -C "$REPO_ROOT" worktree prune
```

Do NOT touch the user's primary working tree. Specifically: do not `git checkout`, `git fetch`, or `git reset` inside `$REPO_ROOT` — the user may be in the middle of unrelated work there. Concurrent ticks reviewing other PRs are running in their own worktrees and must not be disturbed.

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
- Per-PR worktrees mean two ticks claiming PRs #41 and #42 can run concurrently with no shared filesystem state. The `in-review` GitHub label still prevents two ticks from claiming the same PR.
- The user's primary working tree is never touched. If you need to inspect the worktree, it lives at `.claude/worktrees/pr-review/<PR>` until step 10 removes it.
- It deliberately leaves comments **unresolved** so the human PR owner sees them on next visit. Resolution is human-driven.
- Keep ASCII animals small (≤10 lines) when the queue is empty so the output fits in a notification card.
