---
description: Review the implementation and tests against the feature plan, parent PRD, and coding standards. Emits a structured JSON verdict.
argument-hint: (no arguments — consumes $parse-feature-plan.output)
---

# Review implementation

You are reviewing the implementation and tests against the feature plan, the parent PRD, and the project's coding standards. **Read-only.**

Feature plan data (JSON):

```json
$parse-feature-plan.output
```

The test plan path is the `test_plan_path` field of the JSON above.

## Review dimensions

Each finding belongs to exactly ONE dimension.

**(a) Acceptance criteria coverage** — every item in `acceptance_criteria` is exercised by at least one passing test. Missing coverage is `critical`.

**(b) Requirement ID coverage** — every ID in `requirement_ids` appears in at least one test name (unit or E2E). Use grep to verify. Missing tags are `high`.

**(c) Coding standards** (from `CLAUDE.md` and `~/.claude/rules/common/*.md`):
- Immutability — code creates new objects, never mutates inputs. Violations: `high`.
- File size — no implementation file exceeds 800 lines. Violations: `medium`.
- Validation at boundaries — user input / API responses / file content validated before use. Violations: `high`.
- Parameterized SQL only — never string-interpolate values into queries. Violations: `critical`.
- Repository pattern — data access behind an interface, not inline ORM calls scattered in handlers. Violations: `medium`.
- API envelope `{ ok, data, error }` on new endpoints. Violations: `medium`.

**(d) Test quality**:
- Tests describe behaviour, not implementation details. Violations: `medium`.
- No tests asserting on private helpers when a public path covers the same behaviour. Violations: `medium`.
- Tests organised by requirement ID, not by file/module shape. Violations: `low`.
- No tests pinning current buggy output. Violations: `high`.

## Process

1. Use `Read` / `Glob` / `Grep` extensively. Do NOT modify files in this node.
2. For each finding, include: `severity`, `dimension` (a-d), `file` (relative path), `line` (number or null), `requirement_id` (or null), `summary`, `recommendation`.
3. Determine the verdict:
   - `pass` — zero `critical` and zero `high` findings.
   - `needs_fixes` — any `critical` or `high` finding.

## Output

Output **only** a single raw JSON object as your final response. **Do NOT wrap it in a markdown code fence** (no ```` ``` ```` or ```` ```json ````). Your message must start with `{` and end with `}` — nothing before, nothing after.

Expected shape:

```
{
  "verdict": "pass",
  "critical": [],
  "high": [],
  "medium": [],
  "low": [],
  "summary": "..."
}
```
