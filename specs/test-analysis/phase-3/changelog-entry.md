---
feature: changelog-entry
phase: phase-3
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: stub
total_acs: 3
covered: 0
partial: 0
missing: 0
deferred: 3
---

# E2E test analysis: CHANGELOG entry for `0.1.0`

**Spec:** `specs/plans/phase-3/50-feature-changelog-entry.md`
**Implementation status:** stub — `CHANGELOG.md` exists with extensive per-PR entries but contains no `0.1.0` heading. Verified by `grep -n "0.1.0" /Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/CHANGELOG.md` (zero matches). The file uses Keep-a-Changelog format with timestamped sections (`## YYYY-MM-DD HH:MMZ — <title>`) instead of versioned releases. `CHANGELOG.d/` exists with three per-PR fragment files (`2026-05-03T18:43Z-fix-ws-ticket-channel-check-order.md`, `server-max-header-bytes.md`, `server-trusted-proxy-warn.md`) but none represent the `0.1.0` cut. There is no `apps/server/internal/version/version.go` (verified by absence in `ls apps/server/internal/`).
**E2E test directory:** `tests/e2e/phase-3/changelog-entry/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `CHANGELOG.md` contains a `0.1.0` entry dated to the release day. | deferred | — |
| AC-2 | The entry summarizes added features grouped by phase and explicitly references the user stories shipped (US-1 through US-12). | deferred | — |
| AC-3 | Format follows Keep-a-Changelog (`Added` / `Changed` / `Security` sections at minimum). | deferred | — |

## Findings

### Missing E2E tests

None — feature is stub.

### Deferred E2E tests

All 3 ACs deferred. This feature is documentation-only — the test is a static check that the markdown file is in the expected shape. Suggested test file: `tests/e2e/phase-3/changelog-entry/changelog_test.ts` (vitest, per the task brief; static markdown-asserting tests are the canonical case for vitest in this repo).

- **AC-1 (0.1.0 entry exists, dated):** read `CHANGELOG.md` from disk. Assert a heading matching `/^## \[?0\.1\.0\]?\s*[-—]\s*\d{4}-\d{2}-\d{2}/m` exists. The brackets are optional (Keep-a-Changelog uses `## [0.1.0] - YYYY-MM-DD` but the existing repo uses unbracketed `## YYYY-MM-DD ...`); accept both. The date must be valid `YYYY-MM-DD` format and within a reasonable range (`>= 2026-05-01`, `<= today + 1 day`).
  - Also accept the entry living in `CHANGELOG.d/0.1.0.md` (or similar) if the spec ends up using the per-PR fragment pattern documented in CLAUDE.md ("Don't write to conflict-magnet files in feature PRs"). The test should check both locations; pass if either has the entry.
  - Don't pin the exact release date — the spec says "release day" and the test cannot know that in advance. The format check is the AC.
- **AC-2 (US-1 through US-12 referenced + grouped by phase):** parse the `0.1.0` entry's body (everything between the `## 0.1.0` heading and the next `## ` heading). Two assertions:
  - All 12 US identifiers appear at least once: `for (const i of [1..12]) expect(body).toMatch(new RegExp("US-" + i + "\\b"))`. The `\b` word boundary stops `US-1` matching against `US-11`/`US-12`.
  - Phase grouping: assert at least one of "Phase 0", "Phase 1", "Phase 2", "Phase 3" appears as a subheading or bolded label in the entry body. The spec says "grouped by phase"; the exact mechanism (subheading vs bullet prefix) is the implementer's call, but SOME phase grouping must be visible. Accept either `### Phase N` or `**Phase N**` or a bullet starting with `Phase N:`.
- **AC-3 (Keep-a-Changelog sections):** assert the `0.1.0` entry body contains at least the three sections named in the AC: `### Added`, `### Changed`, `### Security`. (The spec says "at minimum" — extra sections like `### Fixed`, `### Removed` are fine and should not fail the test.) Pin the exact `###` heading depth so a `## Added` (wrong nesting, breaks the parser used by changelog-aggregation tools) fails the test.
  - Edge case: the existing per-PR entries in `CHANGELOG.md` use `### Added` / `### Fixed` / `### Notes` etc. The test must scope its assertions to the `0.1.0` section body, not the whole file — otherwise it would pass spuriously on the existing entries.

### Helpers and harness notes

- This is a vitest-friendly task: no server boot, no network, just file read + regex/substring assertions. Mirror the shape of any existing static-content vitest in `tests/` (e.g. `tests/smoke-test/wiring.test.ts` referenced in the cli-full-commands findings doc, if it exists, or otherwise a fresh small file).
- One harness wrinkle: the test must locate the repo root from inside `tests/e2e/phase-3/changelog-entry/` regardless of cwd. Pattern:
  ```ts
  import { fileURLToPath } from "node:url";
  import path from "node:path";
  const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");
  const changelog = await fs.readFile(path.join(repoRoot, "CHANGELOG.md"), "utf-8");
  ```
- Section-body extraction: simplest is `const idx = changelog.indexOf("\n## 0.1.0"); const next = changelog.indexOf("\n## ", idx + 1); const body = changelog.slice(idx, next === -1 ? undefined : next);`. Brittle to alternate heading shapes; if the test grows, swap for a real markdown parser (`unified` + `remark-parse`).
- If the project ends up using the `CHANGELOG.d/` fragment pattern for 0.1.0 (concatenated at release time), the test should `glob` the directory and check that the concatenated content satisfies the ACs, not just `CHANGELOG.md` directly. CLAUDE.md is explicit that `CHANGELOG.md` is a "conflict-magnet file" agents should not edit in feature PRs — so the impl PR for THIS feature is the legitimate exception (it's literally about editing the changelog), but a future flow may aggregate fragments instead.
- No version constant to assert against today (`apps/server/internal/version/version.go` does not exist per the spec's optional implementation step 5). If it lands, add an AC-adjacent test: read the file, assert the constant equals `"0.1.0"`. Defer until/unless that file appears.

## Recommendations for /test-implement

1. Land the test file with all 3 ACs as `it.skip(...)` until the changelog entry is written.
2. After the impl PR adds the `0.1.0` section, un-skip in order: AC-3 (sections present) -> AC-1 (heading + date) -> AC-2 (US references + phase grouping). AC-2 is the most likely to need a follow-up commit if any US ID is missing from the first draft.
3. Don't gate on `apps/server/internal/version/version.go` — the spec marks it as optional. If it lands, add a separate small test asserting the constant.
4. Keep the test framework-light. This is a single small vitest file; resist the urge to add markdown-parsing dependencies until a second similar test justifies them.
5. Cross-feature: `readme-quick-start` AC-5 also references the single-binary feature spec, but that is a README assertion and unrelated to the changelog. No coordination needed.
