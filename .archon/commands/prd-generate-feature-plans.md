---
description: Generate one feature plan markdown file per deliverable from parsed PRD JSON.
argument-hint: (no arguments — consumes $parse-prd.output)
---

# Generate Feature Plans

Generate one feature plan markdown file per deliverable (or logical grouping of related deliverables) for every phase in the parsed PRD data below.

Parsed PRD data (JSON):

```json
$parse-prd.output
```

## Steps

For each phase in the `phases` array:

1. Run `mkdir -p specs/plans/phase-{number}` via Bash.
2. Group the phase's deliverables into features — typically one deliverable = one feature, but combine deliverables when they form one cohesive unit of work.
3. For each feature, write `specs/plans/phase-{number}/feature-{slug}.md` using the Write tool with the content template below.

## File template

```markdown
# Feature: {feature name}

**Parent phase:** [Phase {N}: {phase title}](../phase-{N}-{phase-slug}.md)
**Status:** planned

## Requirements covered
{Bulleted list of `US-N` / `FR-*` IDs this feature implements. Each ID MUST exist in
the `requirements` array of the parsed PRD data — do not invent IDs. Include the
requirement description after the ID, e.g. `- US-3 — User can log in with email`.}

## Acceptance criteria
{Concrete, testable criteria — each bullet starts with a verb. Derive from the
deliverable text and the parent phase's validation_criteria.}

## Implementation steps
{Ordered list of concrete steps a developer would take to build this feature.}

## Test plan
{Bulleted list of test names with the requirement ID(s) they cover, e.g.
`- test_user_can_login — covers US-3, FR-2.1`}

## Files expected to be touched or created
{Bulleted list of relative file paths.}

## Risks
{Feature-specific risks; "None identified" if none.}
```

## Rules

- Use the Write tool to create or overwrite each file (idempotent).
- Slugs are lowercase, hyphenated, derived from the feature name.
- Every `id` in the parsed `requirements` array MUST appear in the `Requirements covered` section of at least one feature plan across all phases. Distribute IDs based on which deliverable the requirement description most naturally maps to.
- Do NOT create files outside `specs/plans/`. Never write to `.agents/plans/`.
- When done, print one line per file written: `wrote: <path>`.
