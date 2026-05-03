---
description: Generate one phase plan markdown file per phase from parsed PRD JSON.
argument-hint: (no arguments — consumes $parse-prd.output)
---

# Generate Phase Plans

Generate one phase plan markdown file per phase from the parsed PRD data below.

Parsed PRD data (JSON):

```json
$parse-prd.output
```

## Steps

1. Run `mkdir -p specs/plans` first via Bash.
2. For each phase in the `phases` array, write a file at `specs/plans/phase-{number}-{slug}.md` using the Write tool with the content template below.

## File template

```markdown
# Phase {number}: {title}

**Status:** planned
**Time estimate:** {time_estimate}
**PRD revision:** {prd_revision}

## Goal
{goal}

## Dependencies
{Phases this depends on. "None" for phase 1; "Phase {N-1}" otherwise unless the PRD
explicitly states a different dependency.}

## Deliverables
{Full deliverables checklist as `- [ ] item` lines, order preserved verbatim from PRD.}

## Validation criteria
{Validation criteria as a bulleted list, verbatim from PRD.}

## Features
{Bulleted list of links to the feature plan files generated for this phase. Use the
same slug convention as the feature-plan generator (lowercase, hyphenated, derived
from each deliverable). Format: `- [feature name](phase-{N}/feature-{slug}.md)`}
```

## Rules

- Use the Write tool to create or overwrite each file (idempotent).
- Slugs are lowercase, hyphenated, derived from the title — strip punctuation, replace spaces with hyphens.
- Do NOT create files outside `specs/plans/`. Never write to `.agents/plans/`.
- When done, print one line per file written: `wrote: <path>`.
