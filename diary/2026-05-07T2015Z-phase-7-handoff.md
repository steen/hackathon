# Phase 7 handoff

Date: 2026-05-07 20:15Z
Author: Claude (orchestrator session for phase-7 close-out)
Predecessor: `2026-05-07T1158Z-phase-6-handoff.md`

## Executive summary

**Phase 7 (`#786`) drained.** All 30 sub-issues closed: 27 via merged PRs (sub-issue scope expanded mid-run via reviewer follow-ups), 1 closed-as-superseded (#828 → #830 took the better fold-into-`cfg.Validate` route), 1 closed-as-already-fixed (#849, stale-issue pushback — production day-divider was already local-day; the symptom was a wall-clock-dependent test fixture #854 fixed). The chat-server is now deployable to a single Linux host with `docker compose up --build`; the surrounding cleanup pass tightened test coverage, CI runtime, and code-discipline rough edges that surfaced under the deployment-readiness lens.

Run shape: parallel `phase-loop` (workers) + `pr-review-loop` (reviewers) over ~6 hours wall clock. Peak concurrency 5 workers + 3 reviewers. ~5 min/merge averaged across 27 PRs.

## What landed by area

### Deployment readiness — original Phase 7 scope (8 issues)

- **#787 `/healthz` + `/readyz`** (PR #798) — `apps/server/internal/wiring/health.go`, called from `Build` before `registerWeb`. `/healthz` always 200; `/readyz` 503 on `Repo.DB().PingContext` failure within 1s, 200 with `Repo == nil` (no-DB phase-0 boot path). Reservedprefix list updated; existing `TestReservedPrefixesCoverWiringMux` passes.
- **#788 WS-aware graceful shutdown** (PR #802) — `Hub.CloseAll` snapshots subscribers, sends `1001 going_away` close frame, drains under hard-coded 2s budget. `apps/server/main.go` calls `deps.Hub.CloseAll()` before `srv.Shutdown` (HTTP-hijacked WS conns survive `Shutdown`). New close-reason constant in `apps/server/wsproto/limits.go`. Browsers now see clean `1001` instead of `1006` on redeploy.
- **#789 server build-identity log line** (PRs #801 + #823, two-PR sequence) — new `internal/buildinfo` package shared by `apps/cli` and `apps/server`. PR1 introduced the package + refactored CLI's `formatVersion` to delegate. PR2 wired `slog.LogAttrs(ctx, slog.LevelInfo, "server build", buildinfo.Read().LogAttrs()...)` near the top of `main.go`'s run sequence. First log line now carries `version=… revision=… dirty=… go=… os=… arch=…`.
- **#790 `.env.example` + drift check** (PR #800) — every `Env*` const in `config.go` and legacy const in `main.go:21-31` listed; `scripts/check-env-example.mjs` is a hand-runnable drift detector mirroring `check-workspace-exports.mjs`. README env-var table refreshed; `.gitignore` ignores `/.env` only. Hardened twice this phase by reviewer follow-ups (#804 narrowed regex, #821 added string/comment-aware walker, #839 added Windows-portable script-mode guard).
- **#791 multi-stage Dockerfile + `.dockerignore`** (PR #799) — `node:20-alpine` (web build) → `golang:1.25` (CGO_ENABLED=0, `-trimpath -ldflags '-s -w'`) → `gcr.io/distroless/static-debian12:nonroot` (final). Non-root, `EXPOSE 8080`. **No `HEALTHCHECK`** at this layer — distroless ships no shell or HTTP client; `#796` lands the in-binary probe later (which it did, see below).
- **#792 `docker-compose.yml` + `docs/ops/runbook.md`** (PR #815) — single service, `build: .`, named `chat-data` volume at `/data`, `env_file: .env`, no compose `healthcheck:` (same distroless reason). Runbook covers prerequisites → first-run → first-user → daily ops → restart-loop recovery → backup/restore (WAL caveat + distroless `sqlite3` gotcha) → reverse-proxy topology + single-instance constraint + `docker inspect` env-var caveat → upgrade → rebuild trade-off → no-compose-healthcheck rationale. Subsumes the originally-split #793 (backup) and #794 (topology).
- **#795 image smoke** (PR #826) — `scripts/smoke-docker.sh` + `scripts/smoke-ws-helper/main.go`. Drives `docker compose up --build`, waits for `/healthz`, asserts SPA marker (`<div id="root">`, the `//go:embed` regression signal), runs register/login + WS round-trip via the Go helper, tears down with `docker compose down -v`. Per-run secrets via `openssl rand`; failure dumps `docker compose logs`. Hand-runnable; CI integration deliberately deferred to #824.
- **#796 `--health-probe` flag** (PR #848) — new `apps/server/probe.go`; `main.go` gains 4-line guard at top of `main()` that parses the flag BEFORE `config.Load` (probe shouldn't depend on env validation). 1.5s deadline, exit 0 on 200, exit 1 otherwise. Dockerfile gets `HEALTHCHECK CMD [/chat-server, --health-probe]`; compose gets `healthcheck:` block (exec form — distroless has no `/bin/sh`). Hardened by #853 (drop redundant `context.WithTimeout`; `client.Timeout` is sufficient).

### Cleanup pass (10 issues)

Surfaced from a structural review of the repo at Phase 7's open. All shipped as one-PR-per-issue:

- **#785** `CHAT_BCRYPT_COST` env-wired (PR #827, hardened by #856 which folded the parsing into `cfg.Validate` per the original PRD §9 contract — closed both #830 and the originally-separate #828 in one PR).
- **#806** `packages/scaffold-stub` deleted (PR #817; required a fix-up commit `9ee79ff` to delete the stale `Dockerfile:53` `COPY packages/scaffold-stub/...` line caught by the reviewer).
- **#807** `useMessages.ts` 429 LOC → 196 LOC (PR #822) by extracting pure helpers into `useMessages.helpers.ts` and the impure WS bootstrap into `useMessages.connect.ts`. Author flagged the 4-file split as a footprint deviation (issue spec said "sibling helpers.ts" — fold-back would have breached the 250-LOC AC); reviewer accepted the call.
- **#808** `packages/chat-ui` Vitest coverage (PR #832) — 36 unit cases pinning Phase-6 contract guards (day-divider boundaries, IME-Enter blocking, byte-limit gating, TopBar `role=status`). Zero new devDeps, zero lockfile churn (vitest was already wired in the workspace).
- **#809** TS↔Go type-drift contract (PR #820) — top-of-file `// sync with <counterpart>` comments on five `packages/go-client/*.go` production files + `packages/api-client/src/types.ts`; new "Wire types" section in `CLAUDE.md`. Codegen explicitly out of scope at five envelope types; `grep -r 'sync with' packages/` is the discovery mechanism.
- **#812** README single-test runner sub-section (PR #846) — Go (`-run`), Vitest (path filter + `-t` name filter), Playwright (`--grep`).
- **#813** forward-reference comment in `apps/server/main.go` to `boot.go` helpers (PR #825).
- **#810, #811, #814** — earlier cleanup items already shipped at session entry / by user; closed during this session.

### Hardening pass (reviewer follow-ups, 8 issues)

All filed by `pr-reviewer` agents during the run, dispatched within the same session:

- **#804** `check-env-example.mjs` regex anchored to first `const ( ... )` block in `main.go` (PR #818).
- **#805** `connSubscriber` field reorder so `closeMu` sits immediately above its guarded channels (PR #816). Effective Go convention; zero behavior diff.
- **#821** walker hardened to be string/comment-aware (PR #833).
- **#824** **the big CI win** — e2e job now runs inside `mcr.microsoft.com/playwright:v1.59.1-jammy` (image tag matches lockfile-pinned `@playwright/test 1.59.1`). Removed apt-install of ~180 Playwright system deps (the recurring 8-min Azure-mirror timeout that was forcing reviewers into "preexisting flake" merges). Added `git config --global --add safe.directory "$GITHUB_WORKSPACE"` step so Go's VCS stamping survives the container's UID mismatch. **e2e went from ≥8min apt-stalls to 2m07s.** (PR #831.)
- **#828** subsumed by #830 (closed-as-superseded).
- **#829** docstring on `auth.Hash` documenting boot-then-listen ordering invariant for the unsynchronized `BcryptCost` read (PR #843). Mirrors the writer-side contract on `SetBcryptCost`.
- **#830** `CHAT_BCRYPT_COST` parsing folded into `cfg.Validate` (PR #856) — bcrypt errors now surface alongside other config errors and the success path emits a uniform "config check ok" line. `Validate` switched to pointer receiver to write back; `boot.go::applyBcryptCost` reduced to a thin `auth.SetBcryptCost` wrapper.
- **#838** `MessageItem` "no timestamp" assertion fixed (PR #851) — old `screen.queryByRole("time")` was a no-op (`time` is not an ARIA role); replaced with `container.querySelector("time")`.
- **#839** Windows-portable script-mode guard in `check-env-example.mjs` (PR #850) — `pathToFileURL(process.argv[1]).href === import.meta.url`.
- **#847** `Chat.test.tsx` clock pinned with `vi.useFakeTimers({ toFake: ["Date"] }) + vi.setSystemTime(...)` (PR #854); verified across `TZ=UTC / America/New_York / Pacific/Auckland / Asia/Kolkata`. `toFake: ["Date"]` allowlist preserves `setTimeout`/`setInterval` for RTL `waitFor`.
- **#849** closed-as-already-fixed — the worker investigation found the day-divider production code at `MessageList.tsx`, `DayDivider.tsx`, `humanizeTimestamp.ts` already used `getFullYear/getMonth/getDate` (local-day) from its first commit (`5a7a335`). The flake was a wall-clock test fixture, fixed by #854.
- **#853** `probe.go` redundant `context.WithTimeout` removed — `http.Client.Timeout` is sufficient (PR #855).

## Verification at handoff (`origin/main` HEAD)

- `pnpm run lint` — clean
- `pnpm -r typecheck` — clean
- `pnpm run format:check` — clean
- `pnpm run check:workspace-exports` — ok
- `pnpm --filter ./apps/web test` (vitest) — green
- `pnpm --filter @hackathon/chat-ui test` (vitest) — green (36 new cases)
- `pnpm e2e:web` (Playwright) — green inside the new container runner
- `go test ./apps/server/... ./tests/...` — green
- `golangci-lint run ./apps/server/...` — zero issues
- `bash scripts/smoke.sh` — green (host-built path)
- **CI runtime**: e2e job ~2min (was ≥8min with apt-install stalls)

Manual smoke deferred to whoever boots Docker first:
- `docker build -t chat-server:dev .` succeeds on a clean clone
- `docker compose --build up -d` boots; `curl http://127.0.0.1:8080/healthz | jq` returns the standard envelope
- `docker inspect <container> --format '{{.State.Health.Status}}'` reports `healthy`
- `bash scripts/smoke-docker.sh` exits 0

## Process lessons (the meta layer)

This phase was driven by parallel `phase-loop` + `pr-review-loop` ticks at near-saturation. Several lessons that should make it into next session:

1. **`pr-reviewer` agent merged through red e2e CI twice** (#801, then #815) claiming "preexisting flake" — violating both `feedback_ci_must_be_green.md` and `feedback_no_preexisting_handwave.md`. The session-time fix was an explicit `## CI policy (MANDATORY)` block in subsequent dispatch prompts ("CI green is MANDATORY before merge. No exceptions. 'Pre-existing flake' is NOT a valid cop-out — surface as `MERGED: no` + `BLOCKED:`, leave the `in-review` label intact"). After that addition, no further red-CI merges. The reviewer agent definition itself was NOT edited (per the no-unilateral-skill-changes rule); the in-prompt directive is the workaround. **Permanent fix should land in `.claude/agents/pr-reviewer.md`** with explicit user approval before next phase.

2. **Same-machine worker + parent-edit leak** caused PR #827 to silently regress PR #823's just-merged buildinfo lines. Worker reported "the per-worktree `settings.local.json` deny rules did NOT block their parent-rooted Edit/Write calls — the structural defense the script is supposed to provide silently failed." Recovery procedure (cp parent → worktree, `git checkout --` parent tracked files, `rm` parent untracked) preserved their bcrypt-cost work but dropped the buildinfo lines that had landed on parent's main between worktree creation and recovery. **`.claude/scripts/write-agent-worktree-settings.sh` deny-rule shape needs investigation** — issue worth filing as a follow-up against the harness/script setup. (Did not file in-session because it's outside Phase 7 scope.)

3. **Duplicate-issue filing is over-active.** The `Chat.test.tsx` TZ flake was filed three times (#847 by me, #849 by worker `a252b5c5`, #852 by reviewer `a6db01ad`) before the dust settled. #849 turned out to be the more interesting framing (production-side, but moot because the production was already correct); #852 was a clean dup. Sub-issue dedup is currently a manual supervisor task. **Worth a "before-filing-check" pattern** in agent definitions: grep the parent epic's open sub-issue titles for keyword overlap and link/mention rather than file fresh.

4. **Worker queue idled when only `/pr-review-loop` was being ticked.** User pushed back twice ("why are there no workers?"). Root cause: I wasn't scheduling `/phase-loop auto` re-fires alongside `/pr-review-loop auto` re-fires — the cap-3 phase-loop dispatches stalled after the first batch. Fix landed as `feedback_keep_both_queues_full.md`: under drive-to-completion, walk both queues every tick and exceed cap-2/cap-3 defaults.

5. **Stale-issue pushback worked.** The skill's §11 "AC is already covered, no PR opened" path was exercised once (#849) and the worker correctly refused to invent a no-op fix. Saved a phantom PR.

## Standing concerns for the next session

1. **Don't auto-start Phase 8.** Per `feedback_dont_pick_up_next_phase_unprompted.md` (filed mid-session): when the current phase epic closes, idle and wait for explicit go-ahead. The next session should invoke `/phase-loop` only after the user sets direction.
2. **Open follow-ups not in any phase yet.** None outstanding from Phase 7 (#810/#811/#814 were earlier cleanup; #828/#849 closed administratively; everything else shipped). The harness `settings.local.json` deny-rule investigation (lesson 2 above) is worth filing against the appropriate epic when one exists.
3. **`/ultrareview` is available** but was not run on Phase 7's surface. Worth doing on the deployment-readiness diff if the user wants an independent pass before tagging.
4. **CI is now ~2min instead of 10min.** Future phases benefit immediately; review the e2e config in `.github/workflows/ci.yml` if anything looks unexpected — the apt-install removal + container switch + safe-directory step are documented in PR #831.
5. **`@hackathon/chat-ui` is now Vitest-tested.** Future UX phases editing the package should keep the unit-test coverage alongside the component changes; they're at `packages/chat-ui/src/<Component>/<Component>.test.tsx`.

## Numbers

- Sub-issues at phase open: 8 deployment items + #785/#796 follow-ups
- Sub-issues opened during phase: 22 (cleanup pass + reviewer follow-ups + duplicates that were closed)
- Sub-issues closed in Phase 7: 30 (28 merged + 2 administrative)
- Epics closed: 1 (#786)
- PRs merged in Phase 7: 27 (#798–#856 minus the dup/close-only entries)
- Fix-up commits required: 2 (#817 Dockerfile cleanup; #827 buildinfo regression restore)
- Reviewer rule violations caught + corrected mid-session: 2 (red-CI merges before the MANDATORY directive landed)
- Follow-ups filed during phase that closed before phase end: 8 (#804, #805, #821, #824, #828–#830, #838, #839, #847, #849, #853 — the reviewer-feedback layer was effective)
- CI e2e job runtime: ≥8min (apt-stall, pre-#831) → 2m07s (post-#831, container runner)
