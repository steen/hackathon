---
feature: readme-quick-start
phase: phase-3
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: stub
total_acs: 5
covered: 0
partial: 0
missing: 0
deferred: 5
---

# E2E test analysis: README quick start

**Spec:** `specs/plans/phase-3/10-feature-readme-quick-start.md`
**Implementation status:** stub — `README.md` is 3 lines (`# Hackathon\nRepo for AI-hackathon May 2026`). No "Quick start" heading, no env-var reference, no `pnpm install` / `pnpm dev` instructions, no single-binary build pointer. Verified by reading `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/README.md`.
**E2E test directory:** `tests/e2e/phase-3/readme-quick-start/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `README.md` includes a "Quick start" section that takes a clean clone to a running app in under 5 minutes (matches Phase 3 validation criterion). | deferred | — |
| AC-2 | Quick start documents the **server** env vars actually read by `apps/server` today (`CHAT_JWT_SECRET`, `CHAT_INVITE_CODE`, `CHAT_DB_PATH`, `CHAT_LISTEN_ADDR`, `CHAT_ALLOW_PUBLIC_BIND`, `CHAT_ALLOWED_ORIGINS`, `CHAT_SERVER_PORT`) with the constraints in `apps/server/internal/config/config.go`. | deferred | — |
| AC-3 | Quick start documents the **client** env var separately: `CHAT_SERVER` is the CLI's base-URL override (consumed by `apps/cli`), not a server var. | deferred | — |
| AC-4 | Quick start shows: `pnpm install` -> `pnpm dev` -> open browser -> register with invite code -> send a message. | deferred | — |
| AC-5 | Mentions the single-binary build (`40-feature-single-binary-demo-verified.md`) and points the reader to it. | deferred | — |

## Findings

### Missing E2E tests

None — feature is stub. All 5 ACs land in "Deferred" until README is rewritten.

### Deferred E2E tests

All 5 ACs are deferred because the README has no quick-start content. When implementation lands, the test shape should be a single Go test file at `tests/e2e/phase-3/readme-quick-start/quick_start_test.go` that:

- **AC-1 / AC-4 (executable-quick-start gate):** read `README.md`, regex out the fenced shell blocks under the "Quick start" heading, exec each command in a temp work dir against `repoRoot(t)`. After the documented `pnpm dev` (or single-binary equivalent) is up, dial a smoke endpoint (e.g. `GET /api/channels` with a freshly-registered token, or `GET /debug/subs?channel=#general`) and assert 200. The point is "the README's commands actually work as written," which is the only honest test of "5 minutes to running app."
  - Run shape: `cmd := exec.CommandContext(ctx, "bash", "-c", block); cmd.Dir = repoRoot(t); cmd.Env = append(os.Environ(), "CHAT_JWT_SECRET=test-secret-32-bytes-min-aaaaaaaa", "CHAT_INVITE_CODE=test-invite", "CHAT_DB_PATH="+tempDB, "CHAT_LISTEN_ADDR=127.0.0.1:0")`. Wait for the listener to bind by polling `/debug/subs` (existing readiness probe).
  - Skip if the README block names a port-bound long-running command — instead extract just the env-var setup and the build/run command, run server in background, kill on test cleanup.
- **AC-2 (server env-var reference):** static check — read `README.md` and assert it contains, for each of the 7 server env-var names listed in the spec, both the var name as a literal string AND the relevant constraint sentence (e.g. for `CHAT_JWT_SECRET` the README must mention "32" and "ASCII" or the `Validate()` denylist). Cross-reference against `apps/server/internal/config/config.go` constants by reading them at test time so the assertions don't drift if the constraint set changes.
- **AC-3 (client env-var section):** static check — assert `README.md` mentions `CHAT_SERVER` in a section distinct from the server vars (e.g. by header proximity or by an explicit "Client" subsection). The spec is explicit that this must not be confused with a server var, so the test should fail if `CHAT_SERVER` appears in the same code block as `CHAT_JWT_SECRET`.
- **AC-5 (single-binary cross-reference):** static check — assert `README.md` contains a link or pointer to the single-binary demo path (either a heading like "Single-binary build" with steps, or an inline reference to `40-feature-single-binary-demo-verified.md` / its Markdown anchor). Cheap regex.

### Helpers and harness notes

- Go is the right language for AC-1/AC-4 even though the spec calls out vitest as an option — re-reading the README, regex-extracting commands, and exec'ing them is straightforward in Go and avoids dragging the vitest harness across a Go-server boot. Static-only ACs (AC-2, AC-3, AC-5) can live in the same Go test file as table-driven cases against `os.ReadFile("README.md")`.
- Steal `repoRoot(t)` from `/Users/steen/Kode/Hackathon/.claude/worktrees/test-agent/tests/server-ws-hub/hub_test.go:307` (walks upward looking for `go.mod`).
- Steal `freePort(t)` from the same file (line 282) for the listener bind.
- Use `randomSecret(t, 32)` from the same file for the test JWT secret rather than the placeholder shown above; the placeholder is fine for documentation but `randomSecret` keeps the test resilient if the dev-default denylist grows.
- Server boot for the executable-README test should shell out to `go build -o bin/server ./apps/server && bin/server` rather than in-process `httptest.Server`, because the AC is about "what the README says works" — and the README will name `go build` (or the single-binary path), not in-process wiring.
- Cleanup: `t.Cleanup(func(){ syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) })` against the bash subprocess group so any backgrounded children (e.g. `pnpm dev` spawning vite + the Go server) all get reaped.

## Recommendations for /test-implement

1. Wait for the README rewrite (this feature's impl PR) to land before opening the test PR — the static checks need a non-empty README to assert against, and the executable check needs a known command list.
2. Land all 5 deferred-AC tests as `t.Skip("README quick-start not implemented yet — see specs/plans/phase-3/10-feature-readme-quick-start.md")` placeholders so the test file exists and turns green automatically once the README PR merges.
3. Wire `tests/e2e/phase-3/readme-quick-start/quick_start_test.go` into the existing `go test ./tests/...` invocation; no new CI job needed.
4. Coordinate with the `40-feature-single-binary-demo-verified` test pass — AC-5 of this feature and AC-3 of that feature both touch README content and should not race.
