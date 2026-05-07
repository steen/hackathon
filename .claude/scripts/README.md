# `.claude/scripts/`

Helper scripts invoked by the supervisor's loop skills and the dispatched
subagents. Kept under `.claude/` so they ship with the agent context.

## `write-agent-worktree-settings.sh`

Run from inside an `issue-pr-worker` / `pr-reviewer` worktree as the FIRST
action in §0, after the `WORKTREE` / `PARENT` capture.

Materializes `<worktree>/.claude/settings.local.json` from
`<parent-repo>/.claude/agent-worktree-settings.template.json`, substituting
`__PARENT_ABS__` with the resolved parent-repo absolute path.

The deny rules block `Edit` / `Write` / `MultiEdit` against the parent's
editable code surface (`apps/`, `packages/`, `specs/`, `tests/`, `scripts/`,
`.github/`, conflict-magnet roots). Project-local settings override user-level
ones, so a broad upstream allow does not defeat this layer.

`MultiEdit` is denied alongside `Edit` and `Write` even though `issue-pr-worker`
and `pr-reviewer` declare `tools: Bash, Read, Edit, Write, Glob, Grep` (i.e. no
`MultiEdit`). The rules are defense-in-depth: a future agent definition that
adds `MultiEdit`, a Claude Code release that changes how `tools:` is enforced,
or a supervisor escalation that grants `MultiEdit` would otherwise bypass the
deny set.

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

## `test-option-a-enforcement.sh`

Adversarial end-to-end test for the deny rules above. Spins up a real temporary
worktree under the parent repo, runs `write-agent-worktree-settings.sh` against
it, and validates the three failure modes from #765: (a) bad-arg invocations
exit non-zero, (b) the rendered `settings.local.json` has the `__PARENT_ABS__`
sentinel fully substituted with the resolved parent abs path in every entry,
and (c) `claude --print` driven against the rendered settings hard-rejects
parent-rooted `Edit` and `Write` tool calls — verified by parsing the
structured `permission_denials[]` array, not by grep'ing prose. Hard fail
(non-zero exit) on any miss. Run directly:
`.claude/scripts/test-option-a-enforcement.sh`. Step (c) requires `claude` and
`jq` on PATH; otherwise it skips with a documented reason.
