#!/usr/bin/env bash
# Materialize a per-worktree .claude/settings.local.json that denies Edit/Write
# against the parent repo's source-code paths and enables the OS-level sandbox.
#
# Run this from inside the agent's worktree as the FIRST action in §0 (after
# capturing $WORKTREE / $TOPLEVEL / $PARENT). Project-local settings override
# user-level ones, so the deny rule survives any broader allow upstream.
#
# Background: `Agent({isolation: "worktree"})` does not chroot Edit/Write —
# parent-rooted absolute paths leak to the parent checkout. See GitHub issue
# #678 for the full research and Anthropic permissions docs for the `//`
# absolute-path syntax.
#
# Why path-specific deny instead of `<parent>/**`: agent worktrees live INSIDE
# the parent repo (at `<parent>/.claude/worktrees/agent-<id>`), so a wildcard
# parent deny would also block the worktree's own edits. We deny the editable
# code surface explicitly; the worktree's mirror of those paths has a different
# absolute prefix and is unaffected.

set -euo pipefail

WORKTREE="${1:-${WORKTREE:-}}"
if [ -z "$WORKTREE" ]; then
  echo "usage: $0 <worktree-abs-path>" >&2
  exit 64
fi

if [ ! -d "$WORKTREE/.git" ] && [ ! -f "$WORKTREE/.git" ]; then
  echo "error: $WORKTREE is not a git worktree" >&2
  exit 65
fi

WORKTREE_ABS="$(cd "$WORKTREE" && pwd -P)"
PARENT="$(git -C "$WORKTREE_ABS" rev-parse --git-common-dir | xargs dirname)"
PARENT_ABS="$(cd "$PARENT" && pwd -P)"

if [ "$WORKTREE_ABS" = "$PARENT_ABS" ]; then
  echo "error: \$WORKTREE equals parent — not a true worktree session, refusing to write deny rules" >&2
  exit 66
fi

TEMPLATE="$PARENT_ABS/.claude/agent-worktree-settings.template.json"
if [ ! -f "$TEMPLATE" ]; then
  echo "error: template not found at $TEMPLATE" >&2
  exit 67
fi

mkdir -p "$WORKTREE_ABS/.claude"
OUT="$WORKTREE_ABS/.claude/settings.local.json"

# Substitute the PARENT_ABS sentinel with the resolved parent path. Use python
# (with json.loads validation) when available; awk otherwise.
PY="${PYTHON:-python3}"
if command -v "$PY" >/dev/null 2>&1; then
  "$PY" - "$TEMPLATE" "$PARENT_ABS" "$OUT" <<'PY'
import json, sys
src, parent, dst = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src) as f:
    raw = f.read()
raw = raw.replace("PARENT_ABS", parent)
json.loads(raw)  # fail fast if substitution broke the JSON
with open(dst, "w") as f:
    f.write(raw)
PY
else
  awk -v parent="$PARENT_ABS" '{ gsub(/PARENT_ABS/, parent); print }' "$TEMPLATE" > "$OUT"
fi

echo "wrote $OUT (parent: $PARENT_ABS)"
