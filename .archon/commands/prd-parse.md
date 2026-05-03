---
description: Parse a PRD markdown file and emit structured JSON of phases, deliverables, and requirement IDs.
argument-hint: <path-to-PRD.md> (defaults to specs/PRD.md)
---

# Parse PRD

You are extracting structured intent from a Product Requirements Document.

The user invoked this workflow with the following input (may contain a PRD path or be empty):

```
$ARGUMENTS
```

## Steps

1. Determine the PRD path. If `$ARGUMENTS` contains an explicit path ending in `.md`, use that. Otherwise default to `specs/PRD.md`. If the file does not exist, fail with a clear error message and stop.
2. Read the PRD file with the Read tool.
3. Locate the **Implementation Phases** section. Match by section title (e.g. "Implementation Phases"), NOT by heading number — heading numbers vary across PRDs.
4. For each phase, extract:
   - `number` (integer)
   - `title` (string)
   - `slug` (lowercase, hyphenated, derived from title — e.g. "Foundation & Auth" → "foundation-auth")
   - `goal` (one or two sentences)
   - `time_estimate` (string, verbatim from PRD)
   - `deliverables` (array of strings, one per checklist item, order preserved)
   - `validation_criteria` (array of strings, verbatim from PRD)
5. Locate the **User Stories / Requirements** section. Match by section title (e.g. "User Stories", "Requirements", "Functional Requirements") — again, NOT by heading number.
6. Extract every requirement ID matching `US-\d+`, `FR-[\w.\-]+`, or `SEC-\d+`, with its description. `SEC-*` IDs typically live under a dedicated **Security Requirements / Acceptance Criteria** section (often a table). Treat them as first-class requirements and include them in the same `requirements` array. Do NOT expand range expressions like `SEC-1…SEC-15` — only emit IDs that appear individually in the PRD.
7. Capture the PRD revision: run `git log -n 1 --pretty=format:%h -- <prd-path>` via Bash to get the short SHA of the last commit that touched the PRD. If git fails or the file is untracked, use the string `"uncommitted"`.

## Output

Output **only** a single JSON object as your final response, with this exact shape:

```json
{
  "prd_path": "specs/PRD.md",
  "prd_revision": "abc1234",
  "phases": [
    {
      "number": 1,
      "title": "Foundation",
      "slug": "foundation",
      "goal": "...",
      "time_estimate": "1 week",
      "deliverables": ["..."],
      "validation_criteria": ["..."]
    }
  ],
  "requirements": [
    {"id": "US-1", "description": "..."},
    {"id": "FR-1.1", "description": "..."},
    {"id": "SEC-1", "description": "..."}
  ]
}
```
