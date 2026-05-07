#!/usr/bin/env bash
# Identify and (optionally) remove stale `.claude/worktrees/agent-*` worktrees.
#
# Default mode is dry-run: actions are printed, nothing is touched. Pass
# `--execute` to perform removals.
#
# Safety contract (these are acceptance criteria from issue #766):
#
#   * Reads `git worktree list --porcelain`. Never parses the human-readable
#     form. Never globs `.claude/worktrees/` as the source of truth.
#   * Removes via `git worktree remove --force <path>` so `.git/worktrees/<id>/`
#     is cleaned atomically. Never `rm -rf`. Never `--force --force` (that
#     would override the dirty guard).
#   * Refuses to touch a worktree whose `git status --porcelain` is non-empty,
#     even with `--execute`. Surfaces it as `SKIP (dirty):` for operator review.
#   * Skips in-flight agents. Extracts the PID from the lock reason
#     (`locked claude agent <id> (pid <N>)`) and uses `kill -0 <N>` to test
#     liveness. Live → silently skip.
#   * Idempotent. A second run with no new staleness produces no actions and
#     exits 0.
#
# Removal eligibility:
#
#   | State                                       | Action                |
#   |---------------------------------------------|-----------------------|
#   | Live PID (kill -0 ok)                       | silent skip           |
#   | Dirty (status --porcelain non-empty)        | SKIP (dirty):         |
#   | Clean + named branch with merged PR         | remove                |
#   | Clean + detached HEAD                       | remove                |
#   | Clean + named branch, no merged PR found    | SKIP (open/unknown)   |
#
# Designed to match the conventions in `write-agent-worktree-settings.sh`.

set -euo pipefail

MODE="dry-run"
for arg in "$@"; do
  case "$arg" in
    --execute) MODE="execute" ;;
    -h|--help)
      cat <<USAGE
usage: $0 [--execute]

Identify and remove stale .claude/worktrees/agent-* worktrees.

Default mode is dry-run; pass --execute to actually remove eligible worktrees.
USAGE
      exit 0
      ;;
    *)
      echo "error: unknown argument: $arg" >&2
      exit 64
      ;;
  esac
done

# Resolve the parent repo's git common dir → its working tree. This script may
# be invoked from any worktree (the parent or an agent worktree); we always
# operate on the parent's full worktree list.
COMMON_DIR="$(git rev-parse --git-common-dir)"
PARENT_ABS="$(cd "$(dirname "$COMMON_DIR")" && pwd -P)"

# Defensive: never propose removing the worktree the script is running from.
SELF_WORKTREE="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -n "$SELF_WORKTREE" ]; then
  SELF_WORKTREE="$(cd "$SELF_WORKTREE" && pwd -P)"
fi

echo "mode: $MODE"
echo "parent: $PARENT_ABS"
[ -n "$SELF_WORKTREE" ] && echo "self: $SELF_WORKTREE"
echo

removed=0
skipped_dirty=0
skipped_unknown=0
skipped_inflight=0
skipped_self=0

# Parse `git worktree list --porcelain`. Records are separated by a blank
# line. Each record always starts with `worktree <abs-path>`; subsequent
# lines may include `HEAD <sha>`, `branch <ref>`, `detached`, `locked [<reason>]`,
# `prunable [<reason>]`. We only need: path, branch (or detached), lock reason.
#
# Use a heredoc + while read so we can call `git`/`gh`/`kill` per record
# without subshelling and losing counters.
porcelain="$(git -C "$PARENT_ABS" worktree list --porcelain)"

# Append a trailing blank line so the final record always flushes.
porcelain="$porcelain"$'\n\n'

wt_path=""
wt_branch=""
wt_detached=0
wt_lock_reason=""

flush_record() {
  local path="$1" branch="$2" detached="$3" lock_reason="$4"

  # Only consider agent worktrees. Everything else (parent, manual worktrees)
  # is left alone.
  case "$path" in
    "$PARENT_ABS"/.claude/worktrees/agent-*) ;;
    *) return 0 ;;
  esac

  if [ -n "$SELF_WORKTREE" ] && [ "$path" = "$SELF_WORKTREE" ]; then
    skipped_self=$((skipped_self + 1))
    echo "SKIP (self): $path"
    return 0
  fi

  # Live-pid check first: regardless of dirty / branch state, an in-flight
  # agent is off-limits.
  if [ -n "$lock_reason" ]; then
    # Match `(pid <N>)`. Use bash regex to avoid an external grep.
    if [[ "$lock_reason" =~ \(pid[[:space:]]+([0-9]+)\) ]]; then
      pid="${BASH_REMATCH[1]}"
      if kill -0 "$pid" 2>/dev/null; then
        skipped_inflight=$((skipped_inflight + 1))
        # Silent per AC, but the count surfaces in the summary.
        return 0
      fi
    fi
  fi

  # Dirty check.
  local dirty
  dirty="$(git -C "$path" status --porcelain 2>/dev/null || echo __ERR__)"
  if [ "$dirty" = "__ERR__" ]; then
    # Worktree path exists in metadata but `git status` failed. Surface it
    # so the operator can investigate (likely a corrupt or already-removed
    # tree the admin dir didn't catch).
    skipped_unknown=$((skipped_unknown + 1))
    echo "SKIP (status failed): $path"
    return 0
  fi
  if [ -n "$dirty" ]; then
    skipped_dirty=$((skipped_dirty + 1))
    echo "SKIP (dirty): $path"
    return 0
  fi

  # Branch / merged-PR check.
  local eligible=0
  if [ "$detached" = "1" ]; then
    # Clean + detached → nothing recoverable.
    eligible=1
  else
    if [ -z "$branch" ]; then
      skipped_unknown=$((skipped_unknown + 1))
      echo "SKIP (no branch / no detached marker): $path"
      return 0
    fi
    local pr_number
    pr_number="$(gh pr list --search "head:$branch" --state merged --json number --jq '.[0].number' 2>/dev/null || true)"
    if [ -n "$pr_number" ]; then
      eligible=1
    else
      skipped_unknown=$((skipped_unknown + 1))
      echo "SKIP (open/unknown PR): $path [$branch]"
      return 0
    fi
  fi

  if [ "$eligible" = "1" ]; then
    if [ "$MODE" = "execute" ]; then
      # Use --force to override the stale lock. The dirty check above is the
      # real gate; never use --force --force.
      if git -C "$PARENT_ABS" worktree remove --force "$path" 2>&1; then
        removed=$((removed + 1))
        echo "REMOVED: $path"
      else
        skipped_unknown=$((skipped_unknown + 1))
        echo "SKIP (remove failed): $path"
      fi
    else
      removed=$((removed + 1))
      echo "[DRY-RUN] git worktree remove --force $path"
    fi
  fi
}

while IFS= read -r line; do
  if [ -z "$line" ]; then
    if [ -n "$wt_path" ]; then
      flush_record "$wt_path" "$wt_branch" "$wt_detached" "$wt_lock_reason"
    fi
    wt_path=""
    wt_branch=""
    wt_detached=0
    wt_lock_reason=""
    continue
  fi
  case "$line" in
    "worktree "*)   wt_path="${line#worktree }" ;;
    "branch "*)     wt_branch="${line#branch refs/heads/}" ;;
    "detached")     wt_detached=1 ;;
    "locked")       wt_lock_reason="(no reason)" ;;
    "locked "*)     wt_lock_reason="${line#locked }" ;;
    *) ;;
  esac
done <<<"$porcelain"

echo
if [ "$MODE" = "execute" ] && [ "$removed" -gt 0 ]; then
  echo "fetching + pruning remote refs"
  git -C "$PARENT_ABS" fetch --all --prune
  echo
fi

if [ "$MODE" = "execute" ]; then
  echo "summary: removed=$removed skipped_dirty=$skipped_dirty skipped_unknown=$skipped_unknown skipped_inflight=$skipped_inflight skipped_self=$skipped_self"
else
  echo "summary (dry-run): would_remove=$removed skipped_dirty=$skipped_dirty skipped_unknown=$skipped_unknown skipped_inflight=$skipped_inflight skipped_self=$skipped_self"
fi
