---
description: Expand the feature plan's test skeleton into a full test plan with unit and E2E tests per requirement ID.
argument-hint: (no arguments — consumes $parse-feature-plan.output)
---

# Create test plan

Expand the feature plan's test skeleton into a full test plan.

Parsed feature plan data (JSON):

```json
$parse-feature-plan.output
```

## Steps

1. Run `mkdir -p specs/plans/phase-{parent_phase.number}/{slug}` via Bash, where `{parent_phase.number}` and `{slug}` come from the JSON above.
2. Use the Write tool to create the test plan at `test_plan_path` from the JSON above (idempotent — overwrite if it already exists).
3. For EVERY requirement ID in `requirement_ids`, list both unit tests and E2E tests. A requirement with no tests is a bug — flag it.
4. Test names MUST carry the requirement ID:
   - Go: `TestUS1_RegisterCreatesUserAndReturnsToken`, `TestFR_1_1_PasswordHashedWithBcrypt`
   - TS/JS: `it('US-1: register creates user and returns token', ...)`, `it('FR-payload-opaque: server never logs payload', ...)`
5. Test names must describe behaviour from the requirement, not implementation details. Tests organised by requirement, not by file/module.

## Test plan file structure

```markdown
# Test plan: {feature_name}

**Feature plan:** [feature-{slug}.md](../feature-{slug}.md)
**Parent phase:** [Phase {N}: {phase title}](../../phase-{N}-{phase-slug}.md)
**PRD revision:** {prd_revision}

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E tests |
|----------------|-------------|------------|-----------|
| US-1 | ... | 2 | 1 |

## Unit tests

### US-1 — {description}

- **Name:** `TestUS1_RegisterCreatesUserAndReturnsToken`
  - **Target file:** `apps/server/internal/service/auth_test.go`
  - **Asserts:**
    - returns 200 with `{ ok: true, data: { token } }` envelope
    - persists user with bcrypt-hashed password
    - rejects duplicate username with `{ ok: false, error }`

### FR-1.1 — {description}
...

## E2E tests

### US-1 — {description}

- **Name:** `it('US-1: user registers, logs in, and accesses protected route', ...)`
  - **Target file:** `tests/e2e/auth.spec.ts`
  - **Scenario:** POST /register → POST /login → GET /me with bearer token
  - **Asserts:** all responses use `{ ok, data, error }` envelope; token is JWT-shaped

## Coverage rules
- Every requirement ID has at least one unit test AND at least one E2E test.
- Test names start with the requirement ID for grep-ability.
- Tests describe behaviour, not implementation — no asserting on private helpers.
```

When done, print `wrote: <path>`.
