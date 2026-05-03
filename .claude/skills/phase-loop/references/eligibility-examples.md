# Eligibility analysis — worked examples

Use these as test cases when filtering sub-issues in §4. The structure is always the same: list the in-flight conflict surface, list the candidate's footprint, decide.

## Example 1 — clean disjoint set

**In-flight PRs:** #80 (`apps/server/internal/{hub,wsapi,http}/`, `main.go`)

**Candidates:**

| Issue | Footprint | Eligible? |
|-------|-----------|-----------|
| #66 (CLI) | `apps/cli/**/*.go` | ✅ disjoint |
| #68 (web) | `apps/web/**` | ✅ disjoint |
| #69 (presence) | server internals | ❌ in flight as #80 |

**Plan:** dispatch #66 + #68 in parallel.

## Example 2 — conflict-magnet collision

**In-flight PRs:** #80 (`apps/server/main.go` among others)

**Candidate:** sec finding "set `MaxHeaderBytes`" — touches `apps/server/main.go`.

**Decision:** ❌ not eligible. The sec fix needs `main.go`, but #80 is also editing it. Concurrent edits collide at squash-merge time. Defer until #80 merges.

## Example 3 — same-package, different files

**In-flight PRs:** #PRA (`apps/server/internal/wsapi/handler.go`)

**Candidate:** sec finding "gate `/debug/subs` to loopback" — touches `apps/server/internal/wsapi/debug_handler.go`.

**Decision:** ✅ eligible. Different files in the same package don't collide. Verify by checking the in-flight PR's `files` array, not just the directory.

## Example 4 — implicit dependency on unmerged code

**In-flight PRs:** #PRB (introduces `packages/api-client`).

**Candidate:** #68 (web app) which imports `@hackathon/api-client`.

**Decision:** ❌ not eligible. The web app needs api-client to be on `main` to install via the workspace. Branching off main today gets a checkout where api-client doesn't exist; the web app build will fail in CI.

**Counter-example:** if #PRB has merged (status `MERGED`, not `OPEN`), the implicit dependency is satisfied. Re-run filtering on every merge event.

## Example 5 — overlapping subagent footprints inside one tick

You picked #66 (`apps/cli/`) as the foreground. You're considering #67 (`packages/go-client/`) as a parallel pick. The CLI imports go-client, but the go-client code already exists on main from PR #76 — the candidate is *modifying* go-client in a separate refactor PR.

**Decision:** ⚠️ defer the parallel pick. Even if #67's footprint is `packages/go-client/` only and the CLI's is `apps/cli/` only, the CLI's tests import go-client. If both subagents are running concurrently and the go-client API surface shifts mid-tick, the CLI tests get unstable. Rule: prefer "no shared imports" disjointness, not just "no shared file paths".

## Example 6 — umbrella issue with multiple findings

**Issue:** #78 (sec audit, 8 findings). Three are eligible (different files), five conflict with #80.

**Decision:** dispatch up to 3 worker subagents, each with `closes_or_refs: Refs` (because closing on one finish would lose the other findings). When the umbrella's last open finding gets fixed, file a follow-up PR that closes #78 explicitly OR have the user close it manually.
