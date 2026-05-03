---
description: Append a Planning entry under [Unreleased] in CHANGELOG.md noting the PRD revision used.
argument-hint: (no arguments — consumes $parse-prd.output)
---

# Update Changelog

Append a "Planning" entry under the `[Unreleased]` section of `CHANGELOG.md` at the repository root, noting the plans were generated from the PRD.

Parsed PRD context:

```json
$parse-prd.output
```

## Steps

1. Check if `CHANGELOG.md` exists at the repo root.
2. If it does **not** exist, create it with the Write tool using this content:

   ```markdown
   # Changelog

   ## [Unreleased]

   ### Planning
   - Generated phase + feature plans from PRD ({prd_path} @ {prd_revision}) — {N} phases, {M} requirements covered.
   ```

3. If it **does** exist, use the Edit tool to add the entry. Two cases:
   - If `### Planning` already exists under `## [Unreleased]`, append a new bullet to it (do not duplicate an entry that already references the same `prd_revision`).
   - Otherwise, insert a new `### Planning` subsection immediately after the `## [Unreleased]` heading.

4. The entry text format:

   `- Generated phase + feature plans from PRD ({prd_path} @ {prd_revision}) — {N} phases, {M} requirements covered.`

5. After editing, print the contents of the `## [Unreleased]` section so the change is visible.
