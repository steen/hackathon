# Feature: CLI `chatd send` and `chatd watch` (no auth)

**Parent phase:** [Phase 0: Walking skeleton, system test ready](../phase-0-walking-skeleton-system-test-ready.md)
**Status:** planned

## Requirements covered
- (no user-story IDs land fully here; US-8 is completed in Phase 2 with the full command set)

## Acceptance criteria
- `chatd send <message>` connects to `/ws`, sends one text frame, exits 0 on success.
- `chatd watch` connects to `/ws` and prints every message it receives to stdout, one per line.
- Server URL is configurable via `--url` flag or `CHAT_SERVER` env var, defaulting to `ws://localhost:PORT/ws`.
- No login flow or token handling exists in this phase.

## Implementation steps
1. Create `apps/cli/main.go` with a subcommand dispatcher (`send` / `watch`).
2. Implement `send`: dial WebSocket, write the joined `args` as a text message, close, exit.
3. Implement `watch`: dial WebSocket, loop reading frames, print each to stdout. Handle SIGINT for clean exit.
4. Read server URL from flag or env var, with a reasonable default.

## Test plan
- Unit test: `send` writes the expected payload to a fake WebSocket server.
- Unit test: `watch` prints frames received from a fake WebSocket server.
- Integration: covered by the smoke-test feature.

## Files expected to be touched or created
- `apps/cli/main.go`
- `apps/cli/cmd/send.go`
- `apps/cli/cmd/watch.go`
- `apps/cli/go.mod`

## Risks
- None identified at this stage; auth and reconnection land in later phases.
