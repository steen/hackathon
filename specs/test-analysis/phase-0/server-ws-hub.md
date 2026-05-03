---
feature: server-ws-hub
phase: phase-0
analyzed_at: 2026-05-03T15:15:00+02:00
analyzed_commit: 206b9e265fadf27b7b59cf0f99e7db941231676a
implementation_status: stub
total_acs: 5
covered: 0
partial: 0
missing: 0
deferred: 5
---

# Test analysis: Server WebSocket endpoint with in-memory hub

**Spec:** `specs/plans/phase-0/feature-server-ws-hub.md`
**Implementation status:** stub — `apps/server/` contains only `doc.go` (`package server` declaration, no `main`, no `/ws` handler, no hub package).

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. | deferred | impl is stub |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. | deferred | impl is stub |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. | deferred | impl is stub |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). | deferred | impl is stub |
| AC-5 | No authentication is required at this stage. | deferred | impl is stub (asserted by absence) |

## Findings

### Deferred tests

All five ACs are deferred until the server implementation lands. Recommended Go test scaffolding:

- **AC-1, AC-4** — integration test: `httptest.Server` wrapping the server's mux, `gorilla/websocket` (or `nhooyr/websocket`) dial against `/ws`, expect 101 upgrade. Layer: Go. Location: `tests/server-ws-hub/server_test.go`.
- **AC-2** — unit test on the hub package: subscribe two clients to `#general`, broadcast, both receive; one unsubscribes, broadcast again, only the remaining one receives. Layer: Go. Location: `tests/server-ws-hub/hub_test.go`.
- **AC-3** — same test scope as AC-2; effectively the same hub behavior.
- **AC-5** — assertion that `/ws` accepts upgrade without an `Authorization` header. Layer: Go. Location: `tests/server-ws-hub/server_test.go`.

A skipped placeholder test per AC has been written so the AC IDs are anchored. When `apps/server` is implemented, the maintainer removes the `t.Skip` and fills in the body using the implementation's actual package paths (likely `hackathon/apps/server/internal/hub`).

### Implementation gap

The spec lists files expected to be created (`apps/server/main.go`, `apps/server/internal/hub/hub.go`, etc.) and Note: spec mentions `apps/server/go.mod` but per `CLAUDE.md` the project uses a single root `go.mod`; the per-app go.mod entry in the spec should be removed.

## Recommendations

1. Skipped test placeholders written under `tests/server-ws-hub/` — keep them in sync as ACs evolve.
2. Update spec to remove the per-app `go.mod` from the "Files expected" list.
3. When implementation begins, the test-analysis agent will detect the unskipped tests on the next run and re-evaluate coverage.
