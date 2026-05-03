# Feature: System smoke test (`scripts/smoke.sh`)

**Parent phase:** [Phase 0: Walking skeleton, system test ready](../phase-0-walking-skeleton-system-test-ready.md)
**Status:** done (PR pending)

## Requirements covered
- (foundational test harness; no user-story IDs map directly)

## Acceptance criteria
- `scripts/smoke.sh` boots the server, starts two `chatd watch` processes, sends a message via `chatd send`, and asserts both watchers received it.
- The script exits 0 on success, non-zero with a clear error message on failure.
- The script tears down all spawned processes on completion (success or failure).
- The script is referenced by the root `package.json` `test` script (or an equivalent task) so it runs as part of the standard test workflow.
- This test stays green for the rest of the project (validation criterion for Phase 0 and Phase 1).

## Implementation steps
1. Create `scripts/smoke.sh` with `set -euo pipefail`.
2. Build server and CLI binaries (or use `go run`).
3. Start the server in the background; record its PID; wait for the listening port to be ready.
4. Start two `chatd watch` processes redirecting stdout to two temp files; record PIDs.
5. Pipe a known message via `chatd send`.
6. Poll the temp files (with a timeout) until both contain the expected message; fail with a clear diff on timeout.
7. Trap EXIT to kill server and watchers, remove temp files.

## Test plan
- The script itself is the test. CI must run it on every commit.

## Files expected to be touched or created
- `scripts/smoke.sh`
- `package.json` (wire `test` script to invoke smoke)
- `.github/workflows/ci.yml` (optional, if CI is being set up in this phase)

## Risks
- Flakiness from race conditions on watcher startup; mitigated by polling for a "ready" log line or the listening port before sending.
