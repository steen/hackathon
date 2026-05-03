# Post-phase-1 server + cli structural cleanup plan

## Scope reminder
- apps/server + apps/cli only (Go).
- Structure-only — no behavior change. Tests must continue to pass byte-identically (where they passed before; #38..#42 add tests that must also pass after merge).
- Single root `go.mod` (`module hackathon`); imports use `hackathon/<path>`.

## Inventory of unmerged branches (verified via `git fetch` + `git log --oneline`)
- `origin/feat/phase-1-auth-endpoints` (#38) — tip `fa22aa1`. Adds: auth handlers (register/login/me/logout/ws-ticket), `internal/auth/middleware.go`, `internal/auth/tickets.go`, `internal/auth/jwt_subject.go`, `internal/http/auth_handlers.go`, `internal/http/auth_store.go`, `internal/http/envelope.go` (DUPLICATE of HEAD's `internal/http/errors.go`), `internal/http/sql_errors.go`, CLI `--ws-ticket` flag.
- `origin/feat/phase-1-ws-hardening` (#39) — tip `81dd110` on top of `fa22aa1`. Single commit. Changes `wsapi.Handler` signature to `Handler(h, ts, cfg)` with origin patterns + ticket redemption.
- `origin/feat/phase-1-rate-limits` (#41) — tip `d529c6e` on top of `fa22aa1`. Adds `internal/ratelimit/{iplimit,userlimit}.go`, `internal/http/middleware_ratelimit.go`, plumbs `UserLimiter` into `AuthDeps`, adds `LogRateLimited` to `authStore`.
- `origin/feat/phase-1-channels-and-messages` (#42) — tip `65a99da` on top of `fa22aa1`. Adds `internal/repo/{channels,messages}.go`, `internal/http/{channels,messages}_handlers.go`, channel-aware `wsapi.Handler` via `?channel=` query param.
- `origin/chore/strict-linters` (#45) — adds `.golangci.yml`, `.prettier*`, `eslint.config.js`, CI workflow, comment-doc fixes across many files.

## Verified pre-existing structural issues on HEAD (before any merge)
- HEAD has BOTH `apps/server/internal/http/errors.go` (declares `Envelope`, `ErrorBody`, `WriteOK(w, data)`, `WriteError(w, code, msg, status)`, plus request-id helpers + `WriteJSON` private writer) AND `apps/server/internal/httpx/errors.go` (declares `Envelope`, `ErrorBody`, `WriteError(w, code, msg, status)`). Verified by `grep -rln 'WriteOK\|WriteError\|ErrorBody' apps/`.
- The `internal/http` envelope is **only used by tests in its own package** plus `internal/http/middleware.go:132` (`Recover` middleware).
- `internal/httpx` is the active production user: `apps/server/main.go:18` imports it for `httpx.BodyCap`; `wsapi.handler.go:31` references `httpx.MessageBodyLimit` in a comment.
- The two packages share both name and type definitions; this would already be a duplication problem without #38.

## Verified envelope signature divergence introduced by #38
- HEAD `internal/http/errors.go:34`: `WriteError(w http.ResponseWriter, code, message string, status int)`
- HEAD `internal/http/errors.go:27`: `WriteOK(w http.ResponseWriter, data interface{})`
- #38 `internal/http/envelope.go:60` (`origin/feat/phase-1-auth-endpoints`): `WriteError(w stdhttp.ResponseWriter, status int, code, message string)`
- #38 `internal/http/envelope.go:55`: `WriteOK(w stdhttp.ResponseWriter, status int, data interface{})`
- #38 also adds `Code*` constants and a public `WriteJSON`.
- All #38 callers use the (status-first, status-required) form. HEAD callers (`middleware.go`, tests) use the (status-last) form.

## Phase A — integration completion

The integration order respects the stack: #38 first, then #39/#41/#42 (peers atop #38) in any order, then #45.

> BULL REVIEW SUMMARY (Phase 2):
> - A.0: real (verified both files exist with same exported types). Caller counts re-confirmed: HEAD's `internal/http.WriteError` has exactly one production caller (`middleware.go:132`) plus `errors_test.go`. #38's shape has 30+ callers in the unmerged tree. Direction (consolidate to #38's shape) is correct, NOT churn — the alternative is to flip 30+ unmerged call sites at merge time which is bigger churn AND harder to get right because they're inside the same merge commit.
> - A.1: real, every conflicting file confirmed via `git merge-tree HEAD origin/feat/phase-1-auth-endpoints`. The `WriteUnauthorized` definition cited at `auth_handlers.go:274` was verified to exist via `git grep`.
> - A.2: real but contains an assumption — see R3 below. Marked as "verify before writing".
> - A.3: real, verified `LogRateLimited`, `UserLimiter` field, and `LoginIPConfig()/RegisterIPConfig()` constructor names by reading `iplimit.go` from the branch.
> - A.4: real, signature merge plan verified against three branch versions of `wsapi/handler.go`.
> - A.5: real, but the lint-fix surface is unknown until run. The plan correctly says "fix only what fires" rather than promising a specific delta.
> - B.1: dropped — the import-cycle reason is genuine; this was bullshit on first writing.
> - B.2: out of scope per Phase 2 rule "behavior change → out of scope". Not bullshit, just correctly bounded.
> - B.3: real but tiny. Will run grep at impl time; if no hits, no commit. That's fine — verification, not churn.
> - B.4: contingent. The trigger ("> 80 lines") is concrete. Acceptable.
> - B.5: housekeeping note, not a separate item.
> - B.6: contingent. Trigger (> 100 lines) is concrete. Acceptable.
> - B.7: dropped — "no action" items are bullshit in a plan. Removing.
> - B.8: dropped — stylistic, out of scope.
> Plan revised below where applicable.

### A.0 — pre-merge structural decision: pick the surviving envelope shape and consolidate
**File: `apps/server/internal/http/errors.go` AND `apps/server/internal/httpx/errors.go`**
**What:** Choose the #38 shape (`WriteOK(w, status, data)`, `WriteError(w, status, code, message)`, `WriteJSON`, `Code*` constants) as the survivor. Reasons:
1. #38 has 30+ call sites in `auth_handlers.go` already on this shape; flipping them would be huge churn.
2. #41/#42 add another ~30 call sites on the same shape (`writeRateLimited`, `channels_handlers.go`, `messages_handlers.go`).
3. HEAD has only 1 production call site (`middleware.go:132 Recover`) and a test file using the old shape.
4. The `Code*` constants exist nowhere on HEAD; deleting #38 would force re-typing the literal strings at every call site.

Concrete pre-merge actions in this commit:
- Rewrite `apps/server/internal/http/errors.go` so its `Envelope`, `ErrorBody`, `WriteOK`, `WriteError`, `WriteJSON` all match the #38 shape, AND port over `WithRequestID`/`RequestID`/`requestIDKey` (the request-id helpers HEAD's file currently owns and #38's `envelope.go` doesn't).
- Update the one production caller `apps/server/internal/http/middleware.go:132` from `WriteError(w, "internal_error", "internal server error", http.StatusInternalServerError)` to `WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")`.
- Update `apps/server/internal/http/errors_test.go:53` and `:16, :34` similarly: `WriteOK(rec, nil)` → `WriteOK(rec, http.StatusOK, nil)`; `WriteError(rec, "bad_request", "missing field foo", http.StatusBadRequest)` → `WriteError(rec, http.StatusBadRequest, "bad_request", "missing field foo")`. Add `Code*` constants too.
- Delete `apps/server/internal/httpx/errors.go` (its `Envelope`/`ErrorBody`/`WriteError` are now duplicates of `internal/http`).
- BUT `internal/httpx` also owns `limits.go` (`BodyCap`, `RESTBodyLimit`, `WriteMessageTooLarge`) and `limits_test.go` references `httpx.Envelope`. Two options:
  - **Option (a) chosen** — Move `internal/httpx/limits.go` into `internal/http/limits.go` (same package, no behavior change). Update `httpx.WriteMessageTooLarge` to use the consolidated envelope. Update `apps/server/main.go:18` from `httpx.BodyCap` to `internal/http`'s `BodyCap`. Update the test file location/imports. Delete the entire `internal/httpx` directory.
  - Option (b) — keep `internal/httpx` as a thin re-export. Rejected: re-exports across packages are exactly the kind of churn-without-value the bullshit review will flag.

> Why (= the non-obvious "why"): two packages declaring the same exported types in the same module guarantees that any caller importing both (e.g. handler tests that need `BodyCap` AND `WriteOK`) gets a name collision; the only way to coexist is to alias one import, which every reader then has to mentally resolve. Consolidating eliminates the collision entirely.

> NOTE: `wsapi/handler.go:31` has a comment "Mirrors httpx.MessageBodyLimit". After consolidation it should read "Mirrors internal/http.MessageBodyLimit". Update.

### A.1 — merge `origin/feat/phase-1-auth-endpoints` (#38)
**Files conflicting per `git merge-tree HEAD origin/feat/phase-1-auth-endpoints`:**
- `apps/server/main.go` — both sides rewrite. Resolution: keep HEAD's bootstrap (config.Load → Validate → resolveListenAddr → BodyCap wrapping), **add** #38's auth wiring (jwtSecret env load, AuthHandlers construction, mux routes for /api/register etc., RequireJWT middleware on /api/me, /api/logout, /api/ws-ticket). Keep HEAD's `wsapi.DebugSubsHandler` mux line.
- `CHANGELOG.md` — keep both timestamped sections (drop conflict markers).
- `scripts/smoke.sh` — out of scope (smoke test). Take #38's version since #38's smoke update is what allows auth-gated WS to still pass.
- `specs/plans/phase-1-persistence-auth.md` — flip `[ ]` to `[x]` for "Auth endpoints" line; merge by hand.

Code conflict resolution for `apps/server/internal/http/envelope.go`: this is a NEW file in #38. Because A.0 has already populated `errors.go` with the same shape + constants, the merge will try to add `envelope.go` and produce a duplicate-symbol compile error. Resolution: delete `internal/http/envelope.go` after the merge (its Envelope/ErrorBody/Code*/WriteJSON/WriteOK/WriteError already live in `errors.go`).

Other #38 files added cleanly (no HEAD counterpart): `internal/auth/middleware.go`, `internal/auth/tickets.go`, `internal/auth/tickets_test.go`, `internal/auth/jwt_subject.go`, `internal/http/auth_handlers.go`, `internal/http/auth_handlers_test.go`, `internal/http/auth_store.go`, `internal/http/sql_errors.go`. Note `auth_handlers.go:274` declares `WriteUnauthorized` — keep it, callers reference it.

CLI: take #38's `apps/cli/cmd/url.go` (`AppendTicket`) and `apps/cli/main.go` (--ws-ticket flag). No conflict.

### A.2 — merge `origin/feat/phase-1-ws-hardening` (#39)
Single commit (`81dd110`) on top of `fa22aa1`. `wsapi/handler.go` conflict expected because HEAD has the body cap + read limit + per-conn rate limit (PRD SEC-6/SEC-8) and #39 has the origin/ticket logic but **drops** those caps.

Resolution: keep BOTH — preserve HEAD's `ReadLimitBytes`/`SendRateBurst`/`SendRatePerSec`/`MessageBodyLimit` consts and `conn.SetReadLimit(ReadLimitBytes)` + the `tokenBucket` + 4 KiB body check inside `readLoop`, AND add #39's `Config{OriginPatterns}` parameter + `Handler(h *hub.Hub, ts *auth.TicketStore, cfg Config)` signature + ticket redemption block + `acceptOpts` use.

Update wiring in `apps/server/main.go`: change `wsapi.Handler(h)` to `wsapi.Handler(h, tickets, wsapi.Config{OriginPatterns: cfg.AllowedOrigins})` (or empty slice if config field absent — verify before writing). Verify `cfg.AllowedOrigins` exists in `internal/config`; if not, pass `nil` and add a TODO `> DEVIATION` line rather than fabricate a config field.

`apps/server/internal/wsapi/handler_test.go` on #39 will need updating to pass the new args; the test file in #39 already does this — accept #39's version.

`scripts/smoke.sh` and `specs/plans/...` — small textual conflicts; resolve by hand.

### A.3 — merge `origin/feat/phase-1-rate-limits` (#41)
Single commit (`d529c6e`) on top of `fa22aa1`. New files (no conflict): `internal/ratelimit/iplimit.go`, `userlimit.go`, both `_test.go`, `internal/http/middleware_ratelimit.go`, `internal/http/middleware_ratelimit_test.go`.

Conflict files (per merge-tree): `apps/server/main.go`, `CHANGELOG.md`, `scripts/smoke.sh`, `specs/plans/...`. Plus `auth_handlers.go` and `auth_store.go` are modified (added `UserLimiter` field, `AuditSink()` method, `LogRateLimited`); these will conflict with the post-A.1 state of those files, but the deltas are small and the diff already verified them.

Resolution for `main.go`: append the rate-limit construction block — `ipLimiter := ratelimit.NewIPLimiter(...)`, `userLimiter := ratelimit.NewUserLimiter(...)`, plumb `UserLimiter` into `AuthDeps`, wrap `/api/login` and `/api/register` mux entries in `httpapi.IPRateLimit(ipLimiter, retryAfter, ah.AuditSink())` middleware. Read #41's main.go for exact construction; do NOT invent constructor names.

`writeRateLimited` (in `middleware_ratelimit.go:62`) calls `WriteError(w, stdhttp.StatusTooManyRequests, CodeRateLimited, ...)` — already in the consolidated shape from A.0, so no signature edit needed.

### A.4 — merge `origin/feat/phase-1-channels-and-messages` (#42)
Single commit (`65a99da`) on top of `fa22aa1`. New files (no conflict): `internal/repo/channels.go` + `_test.go`, `internal/repo/messages.go` + `_test.go`, `internal/http/channels_handlers.go` + `_test.go`, `internal/http/messages_handlers.go` + `_test.go`, `internal/http/ws_broadcast_test.go`.

Conflict files: `apps/server/internal/wsapi/handler.go` (#42 adds `?channel=` routing while HEAD has caps/limits and #39 adds origin/ticket). Resolution: keep #39's `Handler(h, ts, cfg)` signature, keep HEAD's caps, ALSO read `r.URL.Query().Get("channel")` to override `defaultChannel` per #42's pattern. Pass the resolved `channel` into `h.Subscribe`/`h.Unsubscribe`/`readLoop`.

`apps/server/main.go` again — wire up `ChannelsHandlers` and `MessagesHandlers` with `repository` + `h` + `time.Now`. Add `mux.Handle("/api/channels", require(http.HandlerFunc(chh.List)))` etc. Read #42's main.go for exact route names; do NOT invent.

### A.5 — merge `origin/chore/strict-linters` (#45)
Independent off main. Conflict file per merge-tree: `apps/server/main.go` (only). Resolve by re-applying #45's lint-driven edits onto the post-A.4 main.go (mostly errcheck `_ =` discards and gosec `#nosec` markers). Then run `golangci-lint run ./...` and fix any issues the new linters surface in the post-merge code (auth_handlers.go etc. were only lint-clean against the prior config). Expect at least:
- `revive` exported-must-have-comment on the new `Code*`, `AuthEvent*`, `AuthDeps`, `AuthHandlers`, `ChannelsDeps`, `ChannelsHandlers`, `MessagesDeps`, `MessagesHandlers`, `WriteUnauthorized`, `RateLimitAuditSink` etc. — most #38/#41/#42 already have docstrings; spot-check.
- `errcheck` on the `_ = err` swallows in `envelope.go` (now in `errors.go`) — already has `_ = err`, fine.
- `gosec` may flag `time.Now()` callsites or rand usage; address only what fires.

If `golangci-lint` after A.5 produces errors only in `tests/` or non-server/cli files, leave them — they're outside scope. If it produces errors in `apps/server` or `apps/cli`, fix them in this same A.5 commit (do not split — strict-linters by definition includes the lint-fix delta).

### A.6 — green-build verification (no commit; verification step)
After A.0–A.5: run
- `go build ./...`
- `go test -race ./...`
- `golangci-lint run ./...`
- `pnpm -r --if-present test`
- `bash scripts/smoke.sh`
All must exit 0 before declaring Phase A done. If any fail, fix in the smallest possible follow-up commit before starting Phase B.

## Phase B — general structural cleanup (apps/server + apps/cli only)

### B.1 — DROPPED
~~collapse `internal/auth/middleware.go`'s `WriteUnauthorized` injection seam~~

> BULL: dropped. The injection seam exists because `internal/http` imports `internal/auth` (for `auth.RequireJWT`, `auth.UserInfoLookup`, `auth.TicketStore`); reversing that to let `auth/middleware.go` import `internal/http` for the envelope writer creates an import cycle. The current shape is correct. Listing it here only as audit trail.

### B.2 — `apps/cli/main.go` flag parsing duplication vs cobra plan
**File: `apps/cli/main.go:26..56`**
**What:** PRD §7 says the CLI is "built with cobra" (line 188 of PRD: "Built with cobra"). HEAD's main.go is a hand-rolled flag loop with `--url`, `--ws-ticket`, switch-on-args. This is Phase 0 scaffolding still in place at end of Phase 1. The PRD also defines 9 cobra commands (`register`, `login`, `whoami`, `channels`, `channels create`, `send`, `history`, `watch`, `logout`) of which only `send` and `watch` exist.

**Decision: out of scope.** This is feature work, not structural cleanup; the missing commands are Phase 2 deliverables (PRD §12 Phase 2 explicitly lists `apps/cli` "full command set"). Flagging here so the bullshit review doesn't re-raise it.

### B.3 — confirm no `github.com/<org>/...` imports for project paths
**What:** Per CLAUDE.md "Single Go module" rule. Run `grep -rn 'github.com/.*Hackathon\|github.com/.*hackathon\|github.com/steen' apps/ packages/`. If any hit, rewrite to `hackathon/<path>`. Currently no observed violations in the surveyed files; spot-check during implementation.

### B.4 — `apps/server/internal/wsapi/handler.go` post-merge tidy
**File: `apps/server/internal/wsapi/handler.go`**
**What:** After A.4 the `Handler` function will have grown a long parameter list (`h, ts, cfg`) and a long body (origin check + ticket redeem + channel resolve + read limit + caps + rate bucket + readLoop). Verify line count after merge. If > 80 lines, extract `redeemTicket(r, ts) (userID string, status int, err error)` and `resolveChannel(r) string` helpers within the same file. If <= 80, leave alone — splitting for cosmetics is churn.

**Verdict shape: contingent. Concrete trigger = post-merge line count.**

### B.5 — port `internal/httpx/limits_test.go` to `internal/http/limits_test.go`
**File: `apps/server/internal/httpx/limits_test.go` (after A.0 deletion of `internal/httpx`)**
**What:** Already covered as part of A.0 ("Move limits.go ... Update the test file location/imports"). Listed here for traceability; not a new commit.

### B.6 — `apps/server/main.go`: extract auth/rate/channels wiring into a single `wireServer(cfg, repo, hub) http.Handler` function
**File: `apps/server/main.go`**
**What:** After A.4+A.5, main.go's mux-construction block will be ~80 lines of repository/jwtSecret/tickets/handlers/middleware setup. Extracting it into a helper inside the same `package main` reduces `main()` to bootstrap + listen + shutdown.

**Why (non-obvious):** `main` becomes the surface a human reads first when debugging boot order. Inlined wiring forces them to skip past it to find the listen call.

**Trigger:** post-merge `main()` body length > 100 lines. If shorter, drop.

### B.7 — DROPPED
> BULL: "no action" items are pure bullshit in a plan. Removed.

### B.8 — DROPPED
> BULL: log format unification is stylistic, not structural. Out of scope. Removed.

## Open risks / decisions to surface, not bury

- **Risk R1:** A.0 (consolidating envelope shape into `internal/http`) is done **before** merging #38. If a reviewer wants to see #38's shape arrive AS PART OF #38's merge, the alternative is to merge #38 first and then resolve the `envelope.go` vs `errors.go` duplicate-declaration compile error in the merge commit itself. Either order works; the chosen pre-merge consolidation produces cleaner per-commit diffs (the merge commit adds only handlers, not envelope mechanics).
- **Risk R2:** `ratelimit.NewIPLimiter` constructor signature is asserted from `git show origin/feat/phase-1-rate-limits:apps/server/internal/ratelimit/iplimit.go` only at implementation time, not yet read in this plan. If the constructor takes args this plan didn't anticipate, capture the actual signature in a `> DEVIATION` line in the implemented spec rather than fabricating.
- **Risk R3:** `cfg.AllowedOrigins` in `internal/config` may not exist; if absent, A.2 either passes `nil` (degrading to library default same-origin) or this PR adds the field. The latter is borderline behavior change. Default to `nil` + `> DEVIATION` line; surface to reviewer.
- **Risk R4:** strict-linters (#45) may produce errors in `tests/` or `packages/` outside scope. Implementation must NOT silence linters globally to "fix" out-of-scope errors; instead, add per-file `//nolint:` directives only where the linter is wrong, or open a follow-up issue. Since CLAUDE.md forbids "preexisting" hand-waves, follow-up issues must be concrete (file:line + diagnosis), not "look into it later".
