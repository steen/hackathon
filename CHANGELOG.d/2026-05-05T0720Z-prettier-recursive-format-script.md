### Tooling

- Add `pnpm run format` (root) so contributors can apply Prettier across the tree in one command alongside the existing `pnpm run format:check`.
- Narrow `.prettierignore` to drop the `**/*.md` blanket exclude so author-controlled Markdown (e.g. `CHANGELOG.d/*.md` PR fragments) is now part of the CI format gate. Content directories (`specs/`, `diary/`, `linkedin-posts/`, `migrations/`) and AI tooling (`.claude/`) remain ignored, as do the three top-level prose files (`CHANGELOG.md`, `CLAUDE.md`, `README.md`).
- Reformat the four `CHANGELOG.d` fragments that the wider check newly covers; no other source files changed.
