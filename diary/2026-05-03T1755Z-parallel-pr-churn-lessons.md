# Parallel PR churn — what I'd do differently

Date: 2026-05-03 17:55Z
Author: Claude (orchestrator session for phase-1 build-out)
Trigger: user asked, after several hours of cascading rebases, "how would you restructure your work so it becomes easier to land and has less conflicts and churn with an out-of-band review process that reviews, fixes and merges the PR's"

## What I observed today

Phase 1 was implemented as eight parallel feature agents (sqlite, auth-internals, auth-endpoints, body+ws-caps, file-perms+headers, logging+envelope, rate-limits, ws-hardening, channels+messages, startup-config-checks) plus a strict-linter agent and a structural-cleanup agent. Each ran in its own worktree off `main` (or off another open PR's branch in the wave-2 stack).

The result was correct code but a lot of friction:

- **Stacking on open PRs amplified rebases N×.** When PR #38 (auth-endpoints) squash-merged, PRs #39, #41, #42 all flipped to DIRTY in the same minute. Each one needed a rebase against the squashed commit because their original parent commit no longer exists in main's history. The in-review automation handled it, but that's three rebase cycles for one merge.
- **`apps/server/main.go` was a magnet for textual conflicts.** Every feature wires routes onto the mux there. Five parallel features = five conflicts in the same import block and the same `func main()` body. I resolved at least four by hand today.
- **`CHANGELOG.md` collided every time.** Every PR added a timestamped section right under the `## Planned (next)` anchor. Two parallel PRs always conflicted on that line. The agents even invented future timestamps (18:00Z when wall-clock was 14:58Z) to keep newest-first ordering — fabrication driven by the file structure.
- **Two agents independently invented the same envelope.** The logging-and-error-envelope feature shipped `apps/server/internal/http/errors.go` with `Envelope`, `ErrorBody`, `WriteOK`, `WriteError`. The auth-endpoints feature shipped `apps/server/internal/http/envelope.go` with the same types and **different function signatures** (`(w, status, code, msg)` vs `(w, code, msg, status)`). Both have call sites. The structural cleanup PR's first commit (`74b29cb refactor(server/http): consolidate envelope into one package, delete httpx`) is dedicated to undoing this.
- **Linter-after-features means whack-a-mole.** PR #45 (strict golangci-lint + ESLint) was developed against main while phase-1 feature agents wrote new code. When #38 + #41 squash-merged, the linter PR rebased onto code it had never seen — and `gosec G101`, `gocritic exitAfterDefer`, `revive exported`, `gofmt`, etc. fired on the freshly-merged code. I pushed three different fix commits on #45 and the lint job is still failing as of 17:55Z because each new merge brings new findings.
- **Race-fix collision.** PR #36 (focused) wrapped the test buffer as `syncBuf{mu, b}`. PR #38 (drive-by) wrapped it as `safeBuffer{mu, buf}`. Both fix the same goroutine-read-from-shared-`bytes.Buffer` race. The cleanup branch picked one and deleted the other.

## What I'd do differently

The thread is the same: parallel agents collide on shared anchors (files, types, line ranges) when no contract was set up first. Three structural changes would prevent ~80% of what I resolved today.

### 1. Stop stacking PRs on open PRs

Every feature branches off `origin/main`, period. To make this work without serial bottlenecks, cut features small enough to land independently:

- **Phase-friendly slicing**: a "feature" PR introduces a new package or new public function plus its tests. Wiring (route registration, handler binding, env-var reading) lives in a follow-up PR that depends only on `main`.
- **Dead code is fine for one PR cycle**: a package that exports `func Login(...)` but isn't called anywhere yet ships green and merges fast. The wiring PR comes after and is small.
- **No exception for "stacked review" convenience.** Stacking optimizes for *one reviewer reading both at once*; it pessimizes for *parallel agents merging out-of-order and force-rebasing children*.

Cost: 2× the PR count for the same code. Benefit: zero cascading rebases, zero "PR went DIRTY because parent merged" alerts, zero reorder-on-squash conflicts.

### 2. Split the conflict-magnet files before parallelism

`apps/server/main.go` and `CHANGELOG.md` collide every time. The fix is mechanical and should land before any parallel feature work:

- **`main.go` becomes a 20-line bootstrap.** A `routes` package exposes `func All() []Route` and each feature contributes `apps/server/internal/routes/<feature>.go` with an `init()` that appends its `Route{Path, Method, Handler}` to a package-level slice. `main.go` walks that slice once and registers everything. Each feature owns its file; nobody touches main.go.
- **`CHANGELOG.md` becomes `CHANGELOG.d/`.** Each PR drops one Markdown fragment (`CHANGELOG.d/2026-05-03-auth-endpoints.md`). A release script (or CI step) concatenates them on cut. This is the [Towncrier](https://towncrier.readthedocs.io/) pattern; it's been load-tested by half the Python ecosystem. Per-PR files mean zero textual conflicts and no temptation to fabricate timestamps.

Cost: ~50 lines of plumbing once. Benefit: the entire class of shared-anchor conflicts disappears.

### 3. Pre-flight contracts, then parallelize

The duplicate-envelope mess happened because two parallel agents independently decided where to put the same types. The structural-cleanup phase-1 plan that the cleanup agent wrote today (`specs/cleanup/post-phase1.md`, with the bull-review annotations) is exactly the right shape — but it should run **before** code, not after.

Before spawning N parallel feature agents, write a 1-page contract:
- The cross-feature types and their exact exported shapes (`Envelope`, `ErrorBody`, `WriteOK(w, status, data)`, `WriteError(w, status, code, msg)`, the `Code*` constant set).
- The package boundaries (who owns `internal/http`, who owns `internal/auth`, what may import what).
- Naming conventions (`syncBuf` not `safeBuffer`; `RequireJWT` not `AuthMiddleware`).

Each agent's prompt requires reading the contract before writing anything. Bull-reviews the contract before agents spawn, not after they've shipped duplicates.

Cost: ~30 minutes of upfront work per phase. Benefit: no after-the-fact consolidation PR, no diverging signatures, no structural-cleanup phase needed at all.

### Smaller, but worth noting

- **Land the linter as PR #0 of any phase.** Today's `EnvJWTSecret` G101 false positive only surfaced because the linter never tested against #28's code; in the inverse order, every feature agent's code passes lint at PR-open time and the false positives surface once instead of N times. Same logic applies to `gocritic`, `revive`, `gofmt` — lint rules force convergence; impose them first.
- **Don't fabricate timestamps.** Two agents today wrote future UTC timestamps (17:30Z, 18:00Z) into CHANGELOG to maintain newest-first ordering when the actual time was earlier. This violates `CLAUDE.md` "Don't fabricate". Per-file CHANGELOG fragments remove the temptation; absent that, agent prompts must say "use real `date -u +%Y-%m-%dT%H:%MZ`, never invent."
- **Drive-by fixes belong in their own PR.** PR #38 added a "while I was here" race-fix to `tests/cli-send-watch/cli_test.go` that conflicted with PR #36 doing the same fix. Both authors did good work; the conflict was structural. Rule: if you spot a real bug while doing your feature, file a separate PR for it (or push back to whoever's already filed one). Drive-bys multiply collision points.

## Tradeoff to acknowledge

These changes slow down "agent spawn → first PR open" by 5–15 minutes of upfront contract / skeleton work. They cut the in-review automation's mechanical-fix burden by what looks like 60–80% based on what I watched today. Worth it if review-cycle time dominates wall clock; less worth it if you'd rather absorb churn than wait for setup.

For phase 2 onward I'd default to the structured path: contracts first, conflict-magnet files split, linter PR #0, no stacking on open PRs.
