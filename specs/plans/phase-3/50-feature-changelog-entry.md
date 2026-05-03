# Feature: CHANGELOG entry for `0.1.0`

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** planned

## Requirements covered
- (release hygiene; no user-story IDs map directly)

## Acceptance criteria
- `CHANGELOG.md` contains a `0.1.0` entry dated to the release day.
- The entry summarizes added features grouped by phase and explicitly references the user stories shipped (US-1 through US-12).
- Format follows Keep-a-Changelog (`Added` / `Changed` / `Security` sections at minimum).

## Implementation steps
1. Open or create `CHANGELOG.md`.
2. Add a `## [0.1.0] - YYYY-MM-DD` heading.
3. Under `### Added`, list the user-facing capabilities from Phases 0–3 with the relevant US IDs in parentheses.
4. Under `### Security`, list the hardening items from Phase 1 (rate limits, body caps, ws-ticket, headers, etc.).
5. Tag the version in code if a version constant exists (`apps/server/internal/version/version.go`).

## Test plan
- Manual: review the entry against the 12 user stories and Phase 1 hardening list to confirm coverage.

## Files expected to be touched or created
- `CHANGELOG.md`
- `apps/server/internal/version/version.go` (optional)

## Risks
- None identified.
