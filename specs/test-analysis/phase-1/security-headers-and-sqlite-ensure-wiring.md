---
feature: security-headers-and-sqlite-ensure-wiring
phase: phase-1
analyzed_at: 2026-05-03T15:24:52Z
analyzed_commit: 440d18f3597c167bc4b9641f18785fe249d9b69d
implementation_status: stub
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# Test analysis: Wire SecurityHeaders middleware and call EnsureFile at startup

**Spec:** `specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md`
**Implementation status:** stub — spec landed (status: planned), no implementation. `apps/server/main.go` still does not reference `SecurityHeaders` or `EnsureFile`. The `CHAT_DB_PATH`-gated branch in main.go calls `appdb.Open` (which itself calls `EnsureFile` internally — see findings) but that's an indirect wiring shipped by `feature-sqlite-schema-and-ulid` (PR #29), not by this feature.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The HTTP server's `Handler` is built so every response — including those written by `Recover` after a panic and those produced by the `/ws` upgrade path — carries the four SEC-10 headers. | deferred | impl is stub — main.go still uses `Handler: httpx.BodyCap(mux)` with no `SecurityHeaders` wrap. |
| AC-2 | `SecurityHeaders` is layered as the outermost middleware so even error envelopes written by inner layers inherit the headers. | deferred | impl is stub |
| AC-3 | `db.EnsureFile(path)` is invoked from `apps/server/main.go` at startup before any code opens the SQLite file. The path comes from the same env var (`CHAT_DB_PATH`) that the future SQLite open will read. | partial-by-proxy / deferred | `appdb.Open(path)` (used by main.go's CHAT_DB_PATH branch) calls `EnsureFile` internally as of PR #29; the file ends up at 0600 when CHAT_DB_PATH is set. **But** the spec asks for an *explicit* main.go-level call before the open path runs, and the env-var wiring is duplicated rather than shared. The actual end-state behavior the AC cares about (file is 0600 when CHAT_DB_PATH is set) is satisfied; the means is not what the spec specifies. Marking deferred because no test in this feature's scope verifies it. |
| AC-4 | A startup smoke test asserts that, after `main` boots against a fresh temp dir, the configured DB file exists with mode `0600`. | deferred | no `apps/server/main_security_test.go` exists. |

## Findings

### Why "stub" rather than "missing"

The spec was authored as a follow-up to address gaps the test-agent flagged in PR #37 (the `file-perms-and-headers` findings). It is intentionally a *plan*, not an implementation — the same commit that landed the spec also landed the auth-internals feature, which is unrelated. The spec carries `**Status:** planned`. So this is the "spec exists but no impl shipped yet" state, exactly what `deferred` is for.

A future PR (presumably the same one that wires `feature-access-log-fields-and-wiring` referenced in the spec body) will close this. When it lands, the agent will detect the wiring on the next tick and re-promote all four ACs.

### AC-3 is subtle — partial-by-proxy

`appdb.Open` (in `apps/server/internal/db/open.go`) calls `EnsureFile(path)` before `sql.Open`. main.go's `if dbPath := os.Getenv(dbPathEnv); dbPath != "" { sqlDB, err := appdb.Open(dbPath); ... }` therefore *does* result in the DB file being chmod'd to 0600, transitively. So at the *behavioral* level (DB file is 0600), AC-3 is satisfied today.

But the spec is asking for two distinct things:
1. **Explicit invocation from main.go before any code opens the SQLite file.** Currently the call is hidden inside `appdb.Open`. If a future caller goes around `Open` (e.g., a CLI subcommand opens the DB via `sql.Open` directly), the chmod is bypassed.
2. **Single env-var wiring.** Reading `CHAT_DB_PATH` should be done once, at the outermost layer, and the path passed down. Currently main.go reads it for the open path only; if the spec's "before any code opens the SQLite file" is interpreted strictly, calling `EnsureFile` first would also handle the case where someone runs the server with a path that does *not* yet trigger `appdb.Open` (e.g., a future read-only diagnostic mode that opens the file directly).

I chose `deferred` not `partial` because the spec is brand-new and the implementation hasn't been attempted. A `partial` would mean someone tried and missed something.

### Cross-feature dependency

The spec body explicitly calls out coordination with `feature-access-log-fields-and-wiring` (referenced as PR #31 in the spec). That feature's spec doesn't exist on `main` yet — the spec body says "will land at `./feature-access-log-fields-and-wiring.md` once #31 merges". So the suggested handler-chain order `SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(mux))))` depends on a sibling spec that hasn't landed. The fallback ("if this plan lands first, the simpler chain `SecurityHeaders(mux)` is acceptable as an interim") covers the agent's evaluation: even the interim has not been done.

## Recommendations

1. **No tests added by this run** — there is no implementation to anchor tests against, and writing a guaranteed-failing test for a spec the maintainer has explicitly scheduled as follow-up would just put `pnpm test` in a permanently-red state. The earlier PR #37 made the same call; this one continues it.
2. When the implementation lands:
   - Verify `apps/server/main.go` builds the chain with `SecurityHeaders` outermost (so headers ride along on Recover's emergency 500 envelope).
   - Verify `EnsureFile` is called explicitly at main.go level before any DB code path runs, not just transitively via `appdb.Open`.
   - Verify the new `apps/server/main_security_test.go` exists and runs `main`-level checks.
3. **Cross-feature note for the next implementer:** the order specified in the spec (`SecurityHeaders` outermost) requires that `httpx.BodyCap` move from the current outermost slot to inside the chain. The current `Handler: httpx.BodyCap(mux)` becomes `Handler: SecurityHeaders(httpx.BodyCap(mux))` in the interim, or part of the longer `SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(httpx.BodyCap(mux)))))` chain once #31's plan also lands. Order matters: SecurityHeaders MUST be outermost so the 413 envelope from BodyCap also carries the four headers.
