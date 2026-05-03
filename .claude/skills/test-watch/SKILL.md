---
name: test-watch
description: Orchestrator for the independent E2E test agent. One tick — checks lock, pulls main, decides whether there's new work, and if so runs analyze → pick one feature → implement → PR in an isolated worktree. The agent writes E2E tests under `tests/e2e/<phase>/<feature-slug>/` that drive the production system end-to-end. Designed to be fired by `/loop 90s /test-watch`.
user-invocable: true
allowed-tools: [Read, Write, Edit, Bash, Skill]
---

# /test-watch — orchestrator tick

You are the orchestrator for the independent E2E test agent. Each invocation is ONE tick. Most ticks should be a silent no-op. Only do real work when there is a new commit on `origin/main` since the last analyzed commit, OR when the previous analysis identified a feature with missing ACs that hasn't been implemented yet.

The agent owns `tests/e2e/<phase>/<feature-slug>/` exclusively. It writes black-box tests that boot real binaries and drive them via real HTTP/WS clients. It does NOT touch in-package tests, scaffold tests, or any production code. Each tick implements tests for **one feature** — keeps PRs small and reviewable.

All git work happens in a dedicated worktree at `.claude/worktrees/test-agent` so the user's primary working tree, branch, and staged changes are never touched.

## Constants

- `REPO_ROOT`: the main working directory (cwd when this skill is invoked).
- `WORKTREE`: `$REPO_ROOT/.claude/worktrees/test-agent`
- `STATE_FILE`: `$REPO_ROOT/.claude/test-agent/state.json`
- `REST_BRANCH`: `test-agent` — the worktree's idle branch. `main` is owned by the primary tree and cannot be checked out twice; `test-agent` is a local branch that tracks `origin/main` and is what the worktree returns to between runs (so a user who `cd`s into the worktree sees a real branch, not detached HEAD).
- `STALE_LOCK_MINUTES`: 30

## Tick procedure

Run these steps in order. Bail immediately on any step that says "exit silently" — emit no chat output.

### 1. Read or initialize state

If `STATE_FILE` does not exist, create its parent dir and write:
```json
{"in_progress": false, "started_at": null, "last_complete_commit": null, "last_pr_url": null}
```

(The legacy field `last_analyzed_commit` from earlier versions of this skill should be migrated to `last_complete_commit` on first read — they have different semantics now: this one only advances when EVERY feature is fully covered, not just whenever analysis runs.)

Read it. Schema:
- `in_progress` (bool) — concurrency guard
- `started_at` (ISO-8601 string or null) — when the current cycle began
- `last_complete_commit` (sha or null) — last `origin/main` SHA at which analysis showed zero missing ACs across every feature (used to skip analysis when nothing has moved)
- `last_pr_url` (string or null) — PR opened by the most recent successful run

### 2. Concurrency guard

If `in_progress` is `true`:
- If `started_at` is more than `STALE_LOCK_MINUTES` ago: log a one-line warning ("stale test-agent lock from <ts>; clearing") and clear `in_progress` + `started_at`.
- Otherwise: **exit silently** (a previous tick is still running).

### 3. Pre-flight

- `gh auth status` must succeed. If not, write a one-line error to chat ("test-watch: gh CLI not authenticated; skipping") and exit. Do NOT attempt to fix auth.
- If `WORKTREE` does not exist, create it on the rest branch: `git -C "$REPO_ROOT" worktree add -b test-agent "$WORKTREE" origin/main`. If `test-agent` already exists locally (legacy worktree), drop the `-b` flag and just check it out: `git -C "$REPO_ROOT" worktree add "$WORKTREE" test-agent`. If creation fails for any other reason, log the error and exit.
- If `WORKTREE` exists but is not on `test-agent` (e.g. left detached or on a stale feature branch by a prior crashed run), normalize it: `git -C "$WORKTREE" checkout -B test-agent origin/main` (the `-B` form creates or resets the branch in one step). Doing this before step 4 keeps every later step starting from a known shape.

### 4. Detect work

Inside the worktree:
- `git -C "$WORKTREE" fetch --quiet origin main`
- Capture `ORIGIN_SHA=$(git -C "$WORKTREE" rev-parse origin/main)`

If `ORIGIN_SHA == state.last_complete_commit`: **exit silently** (every feature was fully covered the last time we analyzed at this SHA — nothing has moved since).

Then enumerate open agent PRs so the picker doesn't double-write a feature that's already in review:
```
gh pr list --state open --search 'head:test-analysis/' --json number,headRefName,title
```
Parse the title of each open PR for the `<phase>/<feature-slug>` token (the title format is `test(e2e): <slug> — …` per `/test-pr`). Capture this set as `BUSY_FEATURES`.

If unable to query GitHub, log one chat line (`test-watch: gh pr list failed; proceeding without busy-feature filter`) and continue with `BUSY_FEATURES = {}` — the worst case is a duplicate PR that a human can close.

### 5. Acquire lock

Write state with `in_progress=true`, `started_at=<now ISO-8601>`. Keep `last_complete_commit` and `last_pr_url` unchanged.

From this point on, EVERY exit path (success or error) MUST clear `in_progress` and update `started_at=null` before returning.

### 6. Reset worktree to fresh main

```
git -C "$WORKTREE" checkout test-agent
git -C "$WORKTREE" reset --hard origin/main
git -C "$WORKTREE" clean -fd
```

`test-agent` is the rest branch (see Constants). It exists for the lifetime of the worktree and is reset to `origin/main` on every tick.

### 7. Create feature branch

```
SHORT=$(git -C "$WORKTREE" rev-parse --short HEAD)
DATE=$(date +%Y-%m-%d)
BRANCH="test-analysis/${DATE}-${SHORT}"
git -C "$WORKTREE" checkout -b "$BRANCH"
```

If a branch with that name already exists locally (e.g., a prior failed run), append `-2`, `-3`, etc. until unique.

### 8. Run analysis

Invoke the `test-analyze` skill, passing `$WORKTREE` as args. It writes per-feature findings to `$WORKTREE/specs/test-analysis/<phase>/<feature-slug>.md` and updates `$WORKTREE/specs/test-analysis/README.md`.

It returns a summary line:
```
test-analyze: features=<F> total_acs=<T> covered=<C> partial=<P> missing=<M> deferred=<D>
```
followed by one line per feature with missing or deferred ACs:
```
feature: <phase>/<slug> missing=<M> deferred=<D> findings=specs/test-analysis/<phase>/<slug>.md
```

Capture both — you'll use the per-feature lines to pick a target.

If `total_missing == 0`:
- `git -C "$WORKTREE" checkout test-agent` (abandon the empty branch).
- `git -C "$WORKTREE" branch -D "$BRANCH"`
- Update state: `last_complete_commit = ORIGIN_SHA`, `in_progress=false`, `started_at=null`. Persist.
- Exit silently. (No PR when there's nothing to add — every spec'd AC has either an E2E test or a tracked deferred placeholder.)

### 8b. Pick one feature

From the per-feature lines, **filter out any feature whose `<phase>/<slug>` is in `BUSY_FEATURES`** (open agent PRs from prior ticks, captured in step 4). The remaining set is candidate features.

If the candidate set is empty (every feature with missing ACs already has an open PR):
- Abandon the branch as in the silent-exit path above.
- Do NOT update `last_complete_commit` — there's still work, just blocked on review.
- Exit silently.

Otherwise, pick the candidate with the highest `missing` count. Tie-break by highest `deferred`, then alphabetically by `<phase>/<slug>` for determinism.

Capture this as `$FEATURE` (e.g. `phase-1/auth-endpoints`). All test-writing this tick targets only this feature.

### 9. Implement tests for the chosen feature

Invoke the `test-implement` skill with args:
```
worktree=$WORKTREE feature=$FEATURE
```

It reads the findings for `$FEATURE` and writes E2E tests under `$WORKTREE/tests/e2e/<phase>/<feature-slug>/`. It does NOT touch any other directory.

After it returns, run the test suite for the affected E2E directory:
- `cd "$WORKTREE" && go test ./tests/e2e/...`
- `cd "$WORKTREE" && pnpm install --frozen-lockfile && pnpm -r --if-present test` (use `~/.npm-global/bin/pnpm` if `pnpm` is not on PATH)

If any test fails:
- Capture the failure output.
- Append a `## Test run failures` section to the findings doc for `$FEATURE` with the failure log and a one-line interpretation ("E2E reveals real gap in apps/server/... — do not fix here").
- Continue to commit/push/PR — do NOT silently swallow the failure. Failing E2E tests are the agent's primary signal that an AC is not actually met by the implementation. Surface them in the PR body.

If `test-implement` reported `written=0` (e.g. the chosen feature had only deferred ACs and they were all already on record), abandon the branch as in the silent-exit path above.

### 10. Commit and push

```
git -C "$WORKTREE" add specs/test-analysis tests/e2e
git -C "$WORKTREE" commit -m "test(e2e): $FEATURE — $WRITTEN new E2E tests ($PASSING passing, $FAILING failing, $SKIPPED skipped)"
```

(If `commit.gpgsign` is set locally and signing fails for unattended runs, the user can opt into `-c commit.gpgsign=false` for this worktree only via their git config — the orchestrator does not suppress signing on its own. Per CLAUDE.md, never skip hooks/signing without explicit user direction.)

```
git -C "$WORKTREE" push -u origin "$BRANCH"
```

### 11. Open PR

Invoke the `test-pr` skill with args: worktree path, branch name, the single feature implemented, the findings path for that feature, and the test-implement counts. It returns the PR URL.

### 12. Cleanup and persist state

```
git -C "$WORKTREE" checkout test-agent
```

This returns the worktree to its rest branch (which tracks `origin/main`) so a user who later `cd`s into the worktree sees a real branch instead of the just-pushed feature branch or a detached HEAD. The local feature branch can be left alone — git's automatic pruning and the per-tick `reset --hard origin/main` in step 6 will keep things tidy.

Update state:
- `in_progress=false`
- `started_at=null`
- `last_pr_url=<URL from test-pr>`

Do NOT update `last_complete_commit` here — opening a PR at this SHA does not mean every feature is covered. `last_complete_commit` only advances in step 8's silent-exit path when analysis returned `missing == 0` for every feature.

Persist `STATE_FILE`.

Emit a single one-line chat message: `test-watch: opened <URL> (E2E tests for <FEATURE>: <N> written, <F> failing)`.

## Failure handling

Any unexpected error during steps 5–11:
1. Try to leave the worktree in a clean state: `git -C "$WORKTREE" checkout -B test-agent origin/main` (force-resets the rest branch even if it didn't exist or was on something else).
2. Clear the lock: write `in_progress=false`, `started_at=null` to `STATE_FILE`. Do NOT update `last_complete_commit` — we want to retry on the next tick.
3. Emit one chat line: `test-watch: error in step <N>: <short message>`. Do not paste long stack traces — most ticks are unattended.

## Things you must NOT do

- Do not modify production code under `apps/**` or `packages/**`. You only add E2E tests under `tests/e2e/` and findings docs under `specs/test-analysis/`.
- Do not modify tests outside `tests/e2e/<phase>/<feature-slug>/`. The other test directories (`tests/scaffold/`, `tests/smoke-test/`, `tests/server-ws-hub/`, `tests/monorepo-scaffold/`, all in-package `*_test.*`) belong to scaffold maintenance and feature authors — leave them alone.
- Do not implement tests for more than one feature per tick. One feature = one PR.
- Do not modify production code to make a failing E2E test pass. A failing E2E test is the agent's reporting channel — surface it in the PR body and let a human decide whether to fix the impl, refine the test, or amend the spec.
- Do not push to `main` directly. Always go through a feature branch + PR.
- Do not retry destructive git operations (`reset --hard`, `clean -fd`) outside the worktree.
- Do not edit the user's primary working tree.
- Do not skip the lock check or the gh-auth check.
- Do not narrate ticks in chat unless something happened (PR opened, error, stale lock cleared).
