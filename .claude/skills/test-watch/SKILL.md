---
name: test-watch
description: Orchestrator for the test-analysis agent. One tick — checks lock, pulls main, decides whether there's new work, and if so runs analyze → implement → PR in an isolated worktree. Designed to be fired by `/loop 90s /test-watch`.
user-invocable: true
allowed-tools: [Read, Write, Edit, Bash, Skill]
---

# /test-watch — orchestrator tick

You are the orchestrator for the test-analysis agent. Each invocation is ONE tick. Most ticks should be a silent no-op. Only do real work when there is a new commit on `origin/main` since the last analyzed commit.

All git work happens in a dedicated worktree at `.claude/worktrees/test-agent` so the user's primary working tree, branch, and staged changes are never touched.

## Constants

- `REPO_ROOT`: the main working directory (cwd when this skill is invoked).
- `WORKTREE`: `$REPO_ROOT/.claude/worktrees/test-agent`
- `STATE_FILE`: `$REPO_ROOT/.claude/test-agent/state.json`
- `STALE_LOCK_MINUTES`: 30

## Tick procedure

Run these steps in order. Bail immediately on any step that says "exit silently" — emit no chat output.

### 1. Read or initialize state

If `STATE_FILE` does not exist, create its parent dir and write:
```json
{"in_progress": false, "started_at": null, "last_analyzed_commit": null, "last_pr_url": null}
```

Read it. Schema:
- `in_progress` (bool) — concurrency guard
- `started_at` (ISO-8601 string or null) — when the current cycle began
- `last_analyzed_commit` (sha or null) — last `origin/main` SHA we analyzed
- `last_pr_url` (string or null) — PR opened by the most recent successful run

### 2. Concurrency guard

If `in_progress` is `true`:
- If `started_at` is more than `STALE_LOCK_MINUTES` ago: log a one-line warning ("stale test-agent lock from <ts>; clearing") and clear `in_progress` + `started_at`.
- Otherwise: **exit silently** (a previous tick is still running).

### 3. Pre-flight

- `gh auth status` must succeed. If not, write a one-line error to chat ("test-watch: gh CLI not authenticated; skipping") and exit. Do NOT attempt to fix auth.
- If `WORKTREE` does not exist, create it: `git -C "$REPO_ROOT" worktree add "$WORKTREE" main`. If creation fails, log the error and exit.

### 4. Detect work

Inside the worktree:
- `git -C "$WORKTREE" fetch --quiet origin main`
- Capture `ORIGIN_SHA=$(git -C "$WORKTREE" rev-parse origin/main)`

If `ORIGIN_SHA == state.last_analyzed_commit`: **exit silently** (nothing new on main).

### 5. Acquire lock

Write state with `in_progress=true`, `started_at=<now ISO-8601>`. Keep `last_analyzed_commit` and `last_pr_url` unchanged.

From this point on, EVERY exit path (success or error) MUST clear `in_progress` and update `started_at=null` before returning.

### 6. Reset worktree to fresh main

```
git -C "$WORKTREE" checkout main
git -C "$WORKTREE" reset --hard origin/main
git -C "$WORKTREE" clean -fd
```

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

It must return a structured summary back, conceptually:
- `total_features` (int)
- `total_missing_acs` (int)
- `findings_paths` (list of relative paths)

If `total_missing_acs == 0`:
- `git -C "$WORKTREE" checkout main` (abandon the empty branch; it has no commits, so just leaving it unchecked-out is fine)
- `git -C "$WORKTREE" branch -D "$BRANCH"`
- Update state: `last_analyzed_commit = ORIGIN_SHA`, `in_progress=false`, `started_at=null`. Persist.
- Exit silently. (No PR when there's nothing to add.)

### 9. Implement tests

Invoke the `test-implement` skill with `$WORKTREE` as args. It reads the findings and writes new tests under `$WORKTREE/tests/**` matching the existing scaffold patterns.

After it returns, run the full test suite inside the worktree:
- `cd "$WORKTREE" && go test ./...`
- `cd "$WORKTREE" && pnpm install --frozen-lockfile && pnpm -r --if-present test` (use `~/.npm-global/bin/pnpm` if `pnpm` is not on PATH)

If any test fails:
- Capture the failure output.
- Append a `## Test run failures` section to the relevant findings doc with the failure log.
- Continue to commit/push/PR — do NOT silently swallow the failure. The PR will be marked as needing review with the failures called out.

### 10. Commit and push

```
git -C "$WORKTREE" add specs/test-analysis tests
git -C "$WORKTREE" -c commit.gpgsign=false commit -m "test: phase analysis $DATE — $TOTAL_MISSING_ACS new tests"
```

(If `commit.gpgsign` is unset locally, drop the `-c` flag — only suppress signing if the user has set it.)

```
git -C "$WORKTREE" push -u origin "$BRANCH"
```

### 11. Open PR

Invoke the `test-pr` skill with args: worktree path, branch name, findings paths, total counts. It returns the PR URL.

### 12. Cleanup and persist state

```
git -C "$WORKTREE" checkout main
```

Update state:
- `in_progress=false`
- `started_at=null`
- `last_analyzed_commit=ORIGIN_SHA`
- `last_pr_url=<URL from test-pr>`

Persist `STATE_FILE`.

Emit a single one-line chat message: `test-watch: opened <URL> (<N> new tests across <M> features)`.

## Failure handling

Any unexpected error during steps 5–11:
1. Try to leave the worktree in a clean state: `git -C "$WORKTREE" checkout main`.
2. Clear the lock: write `in_progress=false`, `started_at=null` to `STATE_FILE`. Do NOT update `last_analyzed_commit` — we want to retry on the next tick.
3. Emit one chat line: `test-watch: error in step <N>: <short message>`. Do not paste long stack traces — most ticks are unattended.

## Things you must NOT do

- Do not modify production code under `apps/**` or `packages/**`. You only add tests and findings docs.
- Do not push to `main` directly. Always go through a feature branch + PR.
- Do not retry destructive git operations (`reset --hard`, `clean -fd`) outside the worktree.
- Do not edit the user's primary working tree.
- Do not skip the lock check or the gh-auth check.
- Do not narrate ticks in chat unless something happened (PR opened, error, stale lock cleared).
