# Shared: local CI mirror policy

Both `issue-pr-worker` and `pr-rebaser` reference this file. **Never push until every CI job passes locally.** Pushing red is wasted CI minutes plus a public signal that the worker doesn't trust its own gates.

## Read the contract

Read `.github/workflows/ci.yml` before mirroring. The workflow is the source of truth — this file documents the *current* shape; if CI grows a new job tomorrow, this file is wrong until updated.

## The blocks

Run all of them in order. If a block fails, fix in-branch and re-run from the failing block. Skipping a block because "the linter isn't installed" is **never** a valid reason — install it.

### Block A — `go` job

```bash
rtk go build ./...
rtk go test ./...
rtk bash scripts/smoke.sh
```

### Block B — `pnpm` job

```bash
rtk pnpm install --frozen-lockfile
rtk pnpm -r --if-present build
rtk pnpm -r --if-present test
```

### Block C — `lint` job

The CI version of `golangci-lint` is **pinned**. Read the pinned version from the workflow's `golangci/golangci-lint-action@v8` step:

```bash
PINNED_LINT_VERSION="$(grep -E '^\s+version: v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/ci.yml | head -1 | awk '{print $2}')"
test -x "$(go env GOPATH)/bin/golangci-lint" || \
  rtk go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${PINNED_LINT_VERSION}
"$(go env GOPATH)/bin/golangci-lint" run --timeout=5m ./...

rtk pnpm install --frozen-lockfile
rtk pnpm run lint
rtk pnpm run format:check
```

If the local lint version differs from CI, **install the pinned version**. "It runs newer locally" is how false-pass-then-CI-red happens.

### Optional but encouraged

```bash
rtk go test -race <changed-go-packages>   # data races CI doesn't trip under -race
```

## The gate

After all blocks exit 0, confirm to yourself:

> "I ran every CI job locally. Every block exited 0. Every test it would run on GitHub passed on this machine."

Only then `git push`. If you can't make that claim, you don't push.

## Common lint gotchas

- **`bodyclose` on `websocket.Dial`**: capture `resp` and `defer resp.Body.Close()` before `defer conn.CloseNow()`. Tests have a per-file lint suppression in `.golangci.yml`; production code closes it explicitly.
- **`gosec G202` on dynamic `IN (?,?,?)` placeholders**: `//nolint:gosec` with a one-line comment explaining the placeholders are a fixed alphabet of `?,` and no user input enters the SQL string. Bind every id through the args slice.
- **Lint version drift**: CI pins `golangci-lint` in `.github/workflows/ci.yml`. Always install that exact version locally — do not trust `brew install`'s default.
- **`gocritic` / `prealloc` false positives**: many are silenced repo-wide. Read `.golangci.yml` rather than guessing.
- **revive `unused-parameter`**: silenced for `_test.go`. In production code, rename unused parameters to `_`.

## Hard prohibitions

These apply to every agent that uses this policy:

- No push until the local CI mirror is green.
- No `gh pr merge` (or `--auto`).
- No `git push origin main` or `git push --force` to shared branches without explicit per-action authorization. Use `--force-with-lease=<branch>:<expected-sha>` for any rebase + push.
- No `--no-verify` / hook-bypass flags.
- No edits to `apps/server/main.go` or `CHANGELOG.md` unless input authorizes AND no other open PR is touching the same file.
- No `git add -A` (sensitive untracked files leak).
- No invented APIs/paths/line numbers — read first, then cite.
