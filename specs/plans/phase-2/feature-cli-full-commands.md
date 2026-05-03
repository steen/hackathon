# Feature: CLI full command set (channels, history, login, watch, send)

**Parent phase:** [Phase 2: Web UI + shared clients](../phase-2-web-ui-shared-clients.md)
**Status:** planned

## Requirements covered
- US-8 — As a scripter, I want a CLI command, so I can pipe automated notifications into chat.

## Acceptance criteria
- `chatd register <username>` prompts for password and invite code, calls `POST /api/register`, stores the returned token. Exits 0 on success.
- `chatd login` prompts for username and password, stores token in `$XDG_CONFIG_HOME/chatd/config.json` (or platform equivalent).
- `chatd channels` lists channels.
- `chatd history <channel> [--limit N] [--before ID]` prints prior messages.
- `chatd watch <channel>` streams new messages to stdout, with reconnect on disconnect.
- `chatd send <channel> <message>` posts a message; supports stdin input when `<message>` is `-`.
- `chatd whoami` prints the current authenticated username (or exits non-zero with a clear message if not logged in).
- `chatd logout` clears the stored token from the config file and calls the server `POST /api/logout` to invalidate the token server-side. Exits 0 on success.
- All commands authenticate via the stored token and re-use the `packages/go-client` library.
- `--server` flag and `CHAT_SERVER` env var override the default base URL.

## Implementation steps
1. Reorganize `apps/cli` to depend on `packages/go-client` (remove the raw WS code from Phase 0).
2. Implement subcommands using a small dispatcher (e.g., `cobra` or stdlib `flag`-based router):
   - `register`, `login`, `whoami`, `logout`, `channels`, `history`, `watch`, `send`.
3. Implement config persistence: load on startup; write on `login`/`register`; clear on `logout`.
4. Implement reconnect loop in `watch` with capped exponential backoff.

## Test plan
- `test_cli_login_persists_token` — covers US-8 prerequisite.
- `test_cli_channels_lists_channels` — covers US-8.
- `test_cli_history_returns_prior_messages` — covers US-8.
- `test_cli_send_posts_message` — covers US-8.
- `test_cli_watch_receives_real_time_message` — covers US-8 (CLI round-trip).
- `test_cli_send_reads_message_from_stdin_when_dash` — covers US-8 piping use case.
- `test_cli_register_creates_user_and_stores_token` — covers US-1.
- `test_cli_whoami_prints_username_when_logged_in` — covers US-8.
- `test_cli_logout_clears_local_config` — covers US-8.
- `test_cli_logout_then_request_returns_401` — covers SEC-15. Drives `chatd login` → `chatd logout` → `chatd channels` and asserts the second call gets a 401 from the server.

## Files expected to be touched or created
- `apps/cli/main.go`
- `apps/cli/cmd/login.go`
- `apps/cli/cmd/channels.go`
- `apps/cli/cmd/history.go`
- `apps/cli/cmd/watch.go`
- `apps/cli/cmd/send.go`
- `apps/cli/internal/config/config.go`
- `apps/cli/cmd/*_test.go`

## Risks
- Storing the token in a plaintext config file is acceptable for the PRD's threat model (small group of friends) but should be documented in the README.
