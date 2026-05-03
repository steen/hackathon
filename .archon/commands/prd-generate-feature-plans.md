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
2. List existing feature plans for the phase via `ls specs/plans/phase-{number}/feature-*.md 2>/dev/null` to detect prior runs.
3. Determine the phase mode:
   - **Phase 0 — read-only.** Never modify or overwrite any existing `specs/plans/phase-0/feature-*.md` file. If a requirement ID (US/FR/SEC) maps to phase 0 work and is not already covered in an existing phase-0 feature plan, create a NEW `feature-{slug}.md` for the missing piece. Otherwise add no files.
   - **Phases 1–3 — updatable.** You may edit existing feature plans (e.g. to add SEC-N IDs to `Requirements covered`) using the Edit tool, or create new feature files. When editing, preserve unrelated sections; only the `Requirements covered` and `Test plan` sections should change to add traceability.
4. Group the phase's deliverables into features — typically one deliverable = one feature, but combine deliverables when they form one cohesive unit of work.
5. For each feature, write `specs/plans/phase-{number}/feature-{slug}.md` using the Write tool with the content template below (subject to the read-only rule above).

## File template

```markdown
# Feature: {feature name}

**Parent phase:** [Phase {N}: {phase title}](../phase-{N}-{phase-slug}.md)
**Status:** planned

## Requirements covered
{Bulleted list of `US-N` / `FR-*` / `SEC-N` IDs this feature implements. Each ID MUST
exist in the `requirements` array of the parsed PRD data — do not invent IDs. Include
the requirement description after the ID, e.g. `- US-3 — User can log in with email`
or `- SEC-3 — Login response time for unknown user is within 20% of wrong-password time`.}

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

- Use the Write tool to create new files. Use the Edit tool to update existing phase 1–3 files. NEVER overwrite existing phase-0 files.
- Slugs are lowercase, hyphenated, derived from the feature name.
- Every `id` in the parsed `requirements` array (including every `SEC-N`) MUST appear in the `Requirements covered` section of at least one feature plan across all phases. Distribute IDs based on which deliverable the requirement description most naturally maps to.
- Do NOT create files outside `specs/plans/`. Never write to `.agents/plans/`.
- When done, print one line per file touched: `wrote: <path>` for new files, `edited: <path>` for in-place edits, `skipped: <path>` for phase-0 files left untouched.
