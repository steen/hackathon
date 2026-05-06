# `.claude/scripts/`

Helper scripts invoked by the supervisor's loop skills and the dispatched
subagents. Kept under `.claude/` so they ship with the agent context.

## `write-agent-worktree-settings.sh`

Run from inside an `issue-pr-worker` / `pr-reviewer` worktree as the FIRST
action in §0, after the `WORKTREE` / `PARENT` capture.

Materializes `<worktree>/.claude/settings.local.json` from
`<parent-repo>/.claude/agent-worktree-settings.template.json`, substituting
`PARENT_ABS` with the resolved parent-repo absolute path.

The deny rules block `Edit` / `Write` / `MultiEdit` against the parent's
editable code surface (`apps/`, `packages/`, `specs/`, `tests/`, `scripts/`,
`.github/`, conflict-magnet roots). Project-local settings override user-level
ones, so a broad upstream allow does not defeat this layer.

`sandbox.enabled: true` adds OS-level cwd-write scoping for the Bash bypass
class (sed / python / echo redirects) that PreToolUse:Edit hooks can't catch
(Anthropic issue #29709).

Why path-specific deny instead of `<parent>/**`: agent worktrees live INSIDE
the parent repo at `<parent>/.claude/worktrees/agent-<id>`, so a parent-wide
deny would also block writes to the worktree itself (deny overrides allow in
the rule engine). Denying the editable code paths explicitly avoids the
collision; the worktree's mirrors of those paths have a different absolute
prefix and remain writable.

This is **not** a chroot — it's a permission-rule layer on top of the existing
`isolation: "worktree"` chdir. The agent's RULE 0 prose remains as
defense-in-depth (a model that drifts on the deny rules' coverage still trips
RULE 0 first).
