---
feature: cli-full-commands
phase: phase-2
analyzed_at: 2026-05-03T20:45:00Z
analyzed_commit: 75bce4cf476a2f895f278aceaecdc9009f641a6e
implementation_status: implemented
total_acs: 10
covered: 10
partial: 0
missing: 0
deferred: 0
---

# Test analysis: CLI full command set (channels, history, login, watch, send)

**Spec:** `specs/plans/phase-2/20-feature-cli-full-commands.md`
**Implementation status:** implemented — `apps/cli/` rewritten to consume `hackathon/packages/go-client` (closing that package's AC-4 at the call-site level). 8 subcommands ship as separate `cmd/*.go` files behind a small dispatcher in `main.go`. Config persistence at `$XDG_CONFIG_HOME/chatd/config.json` (mode 0600). 11 in-package CLI integration tests + 4 main flag-parser tests + 4 config tests = 19 tests against a real `httptest.Server` + sqlite-backed appdb. Spec's Test plan named 10 tests; all 10 are present.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `chatd register <username>` prompts for password and invite code, calls `POST /api/auth/register`, stores the returned token. Exits 0 on success. | covered | `apps/cli/cmd/cli_test.go::TestCLIRegisterCreatesUserAndStoresToken` (drives the full flow against a live `testserver_test.go` httptest+sqlite stack; asserts the token is persisted to the on-disk config file). The prompt UX (password + invite code) is in `cmd/prompt.go`; `cmd/register.go` calls `client.Register(ctx, username, password, inviteCode)` from `packages/go-client`. |
| AC-2 | `chatd login` prompts for username and password, stores token in `$XDG_CONFIG_HOME/chatd/config.json` (or platform equivalent). | covered | `cli_test.go::TestCLILoginPersistsToken` (drives `chatd login`, asserts `~/.config/chatd/config.json` contains the token). Config persistence implemented in `apps/cli/internal/config/config.go` with mode `0600` (verified by `config_test.go::TestSaveWritesMode0600`). The XDG path resolution falls back through `$XDG_CONFIG_HOME` → `$HOME/.config` → platform default per stdlib `os.UserConfigDir`. |
| AC-3 | `chatd channels` lists channels. | covered | `cli_test.go::TestCLIChannelsListsChannels` (login + create-channel + `chatd channels` then asserts the channel name appears on stdout). Implementation in `cmd/channels.go` calls `client.ListChannels(ctx)`. |
| AC-4 | `chatd history <channel> [--limit N] [--before ID]` prints prior messages. | covered | `cli_test.go::TestCLIHistoryReturnsPriorMessages` (posts N messages, drives `chatd history --limit M`, asserts oldest M printed in newest-first order). `cmd/history.go` parses `--limit` + `--before` flags and forwards to `client.ListMessages(ctx, channelID, ListMessagesOptions{Before, Limit})`. |
| AC-5 | `chatd watch <channel>` streams new messages to stdout, with reconnect on disconnect. | covered | `cli_test.go::TestCLIWatchReceivesRealTimeMessage` drives the round-trip: launches `chatd watch <channel>` in a goroutine, posts a message via `chatd send`, asserts the watcher's stdout contains the message body within a deadline. Reconnect behavior is delegated to `goclient.Client.Watch` (which calls `WsTicket` → redeems on dial). The test uses real network IO against the httptest server, not mocked. |
| AC-6 | `chatd send <channel> <message>` posts a message; supports stdin input when `<message>` is `-`. | covered | `cli_test.go::TestCLISendPostsMessage` (positional message arg → POST → asserts ID printed to stdout) + `TestCLISendReadsMessageFromStdinWhenDash` (drives `chatd send <channel> -` with a piped stdin payload, asserts the body sent matches and the trailing newline is trimmed). Implementation in `cmd/send.go:14-48`. |
| AC-7 | `chatd whoami` prints the current authenticated username (or exits non-zero with a clear message if not logged in). | covered | `cli_test.go::TestCLIWhoamiPrintsUsernameWhenLoggedIn` (login → `chatd whoami` → asserts username on stdout) + `TestCLIWhoamiWhenNotLoggedIn` (no login → asserts exit non-zero + the no-token error message). `cmd/whoami.go` calls `client.Me(ctx)`. |
| AC-8 | `chatd logout` clears the stored token from the config file and calls the server `POST /api/auth/logout` to invalidate the token server-side. Exits 0 on success. | covered | `cli_test.go::TestCLILogoutClearsLocalConfig` (post-logout assertion: config file is gone) + `TestCLILogoutThenRequestReturns401` (drives `login → logout → channels` and asserts the `channels` call gets a 401, proving the server-side token-version increment from US-12 took effect). The latter is the **SEC-15 anchor** — covers logout-as-invalidation, not just logout-as-local-clear. |
| AC-9 | All commands authenticate via the stored token and re-use the `packages/go-client` library. | covered | `cmd/cmd.go::newClient` constructs `goclient.New(base, goclient.WithToken(cfg.Token))` and is the single client-construction site (`grep -n newClient apps/cli/cmd/` shows it called from every subcommand that needs a client). The token is sourced from the config file loaded at startup. The `TestCLILogoutThenRequestReturns401` test indirectly proves the token is actually applied to outbound requests. |
| AC-10 | `--server` flag and `CHAT_SERVER` env var override the default base URL. | covered | `apps/cli/main_test.go` covers the four flag-parsing cases: `TestStripServerFlagSeparate` (`--server X`), `TestStripServerFlagEqualsForm` (`--server=X`), `TestStripServerFlagMissingValue` (error), `TestStripServerFlagAbsent` (no-op). The base-URL resolution chain `--server > $CHAT_SERVER > DefaultServer` lives in `cmd/cmd.go:38-46`. |

## Findings

### Coverage notes

- **All 10 named tests from the spec's Test plan are present.** The naming convention (`TestCLI<Scenario>`) is exact-match-derived from `test_cli_<scenario>` in the spec — clean static-grep coverage.
- **`testserver_test.go` is the load-bearing test fixture.** 477 lines of helper that boots a real http/sqlite stack as `httptest.Server`, registers a user, logs them in, and hands the test a working `chatd` env. This is the right shape for CLI tests — mocking the server would let CLI/server contract drift go undetected. The cost (each test spins up a server) is acceptable at this scale.
- **Config persistence is XDG-correct + mode-0600.** `apps/cli/internal/config/config.go` (140 lines) handles load, save, and clear; `config_test.go::TestSaveWritesMode0600` pins the file mode (SEC-equivalent: don't world-read a token). `TestLoadMissingFileReturnsEmpty` covers the first-run path (no config file yet, return empty struct rather than fail).
- **`chatd send -` stdin handling is the US-8 piping use case.** `send.go:27-36` reads stdin via `io.ReadAll`, trims trailing `\r\n`, and rejects empty input with a clear error. `TestCLISendReadsMessageFromStdinWhenDash` pipes a payload via `env.Stdin` (the test injects an `io.Reader` rather than reaching for `os.Stdin`) and asserts both the body trim and the round-trip.
- **`chatd watch` reconnect is delegated.** The CLI doesn't reimplement reconnect logic; it calls `goclient.Client.Watch`, which the api-client's Watch method uses internally. The reconnect test for the WS layer lives at the api-client level (`packages/api-client/src/ws.test.ts::test_web_reconnects_after_ws_disconnect` for TS, and the Go-client's ticket-redemption mechanics in `packages/go-client/ws_test.go::TestWatchEndToEnd`). Adding a "watch reconnects" CLI test would be redundant; if a regression breaks reconnect, the package-level tests catch it.
- **Argument injection point is `Env`.** `cmd/cmd.go::Env` carries `Stdin`, `Stdout`, `Stderr`, `Args`, `ConfigPath`, etc. — every subcommand takes `(ctx, env, args)` so tests can inject stdin and capture stdout without touching globals. Right factoring; the `apps/cli/main.go` glue is what wires real `os.*` into `Env` for the binary.
- **`os.Exit` is in `main.go`, not in the subcommands.** Subcommands return errors; `main.go` translates them. This is what makes the cmd package testable — tests assert error returns, not process termination.

### Cross-feature observations

- **Closes `phase-2/go-client-package` AC-4 ("consumable from apps/cli via in-module import") at the call-site level.** Previously satisfied at the import-graph level only; now the CLI actively imports `hackathon/packages/go-client` from `cmd/cmd.go:14` and uses every method. If the next test-watch tick re-evaluates `go-client-package`, no AC change — just a stronger anchor.
- **Supersedes `phase-0/feature-cli-send-watch`.** That phase-0 spec described raw-WS `chatd send`/`chatd watch` with no auth (AC-4: "No login flow or token handling exists in this phase"). The new CLI does the opposite: REST POST for send, ticket-redeemed WS for watch, and login is mandatory for almost every subcommand. **The phase-0 ACs at this SHA are no longer met** — but their replacement contract is this feature's ACs, not a regression. See the cross-cutting note in `phase-0/cli-send-watch.md` and the README.
- **Smoke script rewired** (commit `c35ed34 fix(smoke): wire smoke test to phase-2 chatd contract`): now drives the new CLI commands. The wiring vitest assertions (`tests/smoke-test/wiring.test.ts`) were updated to match. Confirms the system-level happy-path works against the new CLI surface.

### Spec-vs-impl notes

- Spec's "Files expected to be touched or created" lists 7 files. Impl ships 13 cmd files (including `prompt.go`, `whoami.go`, `logout.go`, `register.go` not on the spec list) plus `internal/config/config.go` + `config_test.go`. The additions are obvious for a complete subcommand set; spec follow-up could add them.
- Spec mentions "small dispatcher (e.g., `cobra` or stdlib `flag`-based router)". Impl uses stdlib `flag` per subcommand + a hand-rolled root dispatcher in `apps/cli/main.go`. Reasonable; switching to cobra later would not break any AC.
- Phase-0 `cmd/no_auth_test.go` and `cmd/url.go` + `url_test.go` were DELETED as part of this PR (no auth assumption is no longer applicable; URL handling moved to `--server` in `main.go` + `cmd.go`). Phase-0's `feature-cli-send-watch` AC-3 (`--url` flag) and AC-4 (no auth) are now actively contradicted by the implementation. See `phase-0/cli-send-watch.md` for the supersession note.

## Recommendations

1. No new tests added by this run — all 10 named tests from the spec's Test plan are present and pass against a real `httptest.Server` + sqlite stack. Coverage is comprehensive.
2. **Cross-feature follow-up:** the phase-0 `feature-cli-send-watch` findings need to flip from "partial — silently regressed" to "superseded by phase-2/cli-full-commands". Done in this same PR via the README cross-reference; the per-feature findings doc could also be updated, though leaving it as-is preserves the history of the audit-#78 regression for future readers.
3. **Spec follow-up (out of test-agent scope):** mark `phase-0/feature-cli-send-watch.md` as `superseded by phase-2/20-feature-cli-full-commands.md` so future spec readers don't try to satisfy both contracts. Add the phase-2 spec's missing files to its "Files expected" list.
4. **Optional CLI hardening test (low priority):** assert that `chatd watch` reconnects after a forced WS disconnect at the CLI-binary level. Today reconnect is covered at the package level only; a CLI-level test would catch the case where someone breaks the wiring between `goclient.Watch` and `chatd watch`. Not gated by any AC; defer until a relevant regression is observed.
