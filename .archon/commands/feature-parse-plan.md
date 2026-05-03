---
description: Parse a feature plan markdown file and emit structured JSON of the feature, its requirement IDs, parent phase, and parent PRD context.
argument-hint: <path-to-feature-plan.md>
---

# Parse feature plan

You are extracting structured intent from a feature plan markdown file.

The user invoked this workflow with the following input (must contain a feature plan path):

```
$ARGUMENTS
```

## Steps

1. Determine the feature plan path. If `$ARGUMENTS` contains an explicit path ending in `.md`, use that. Otherwise fail with a clear error: the workflow requires a feature plan path.
2. If the file does not exist, fail with a clear error message and stop.
3. Read the feature plan file with the Read tool.
4. Read `CLAUDE.md` and any `~/.claude/rules/common/*.md` files so downstream nodes inherit coding-standard context (immutability, files <800 lines, validation at boundaries, parameterized SQL, repository pattern, requirements-not-implementation coverage).
5. Extract:
   - `feature_name` (string, from the `# Feature:` heading or first H1)
   - `slug` (lowercase, hyphenated, derived from the file name `feature-{slug}.md`)
   - `parent_phase` (object with `number`, `slug`, and `path` to the phase plan markdown, parsed from the "Parent phase" link in the feature plan)
   - `requirement_ids` (array of strings — every `US-\d+` or `FR-[\w.\-]+` ID listed under "Requirements covered")
   - `acceptance_criteria` (array of strings, verbatim from the plan, each must be testable)
   - `implementation_steps` (ordered array of strings)
   - `test_plan_skeleton` (verbatim contents of the "Test plan" section, may be empty)
   - `files_to_touch` (array of relative paths from "Files expected to be touched or created")
   - `risks` (array of strings; empty array if "None identified")
6. Locate the parent PRD. Default to `specs/PRD.md`. If the feature plan or phase plan references a different PRD path, use that. If the file does not exist, fail.
7. Read the PRD and, for each `requirement_id`, capture its description text. Emit them as `requirements: [{"id": "US-1", "description": "..."}]`.
8. Capture the PRD revision: run `git log -n 1 --pretty=format:%h -- <prd-path>` via Bash. Use the literal string `"uncommitted"` if git fails or the file is untracked.

## Output

Output **only** a single raw JSON object as your final response. **Do NOT wrap it in a markdown code fence** (no ```` ``` ```` or ```` ```json ````). Your message must start with `{` and end with `}` — nothing before, nothing after.

Expected shape:

```json
{
  "feature_plan_path": "specs/plans/phase-1/feature-auth.md",
  "feature_name": "Auth",
  "slug": "auth",
  "parent_phase": {
    "number": 1,
    "slug": "foundation",
    "path": "specs/plans/phase-1-foundation.md"
  },
  "test_plan_path": "specs/plans/phase-1/auth/test-plan.md",
  "prd_path": "specs/PRD.md",
  "prd_revision": "abc1234",
  "requirement_ids": ["US-1", "FR-1.1"],
  "requirements": [
    {"id": "US-1", "description": "..."},
    {"id": "FR-1.1", "description": "..."}
  ],
  "acceptance_criteria": ["..."],
  "implementation_steps": ["..."],
  "test_plan_skeleton": "...",
  "files_to_touch": ["..."],
  "risks": ["..."]
}
```

`test_plan_path` is the canonical location downstream nodes will read/write: `specs/plans/phase-{parent_phase.number}/{slug}/test-plan.md`.
