### Added

- **tests/e2e/phase-8/channel-ws-drift**: cross-client e2e canary that
  fails CI when the `{type:"channel"}` WS frame drifts between
  `packages/go-client/ws.go` and `packages/api-client/src/types.ts`. A
  Go subprocess (`cmd/wsobserver`) decodes inbound frames through the
  typed `goclient.Watch` surface; the test process subscribes via the
  TS `WebSocketClient`. Both observers consume the same server-emitted
  frame for `POST /api/channels` (`kind:"create"`) and
  `PATCH /api/channels/{id}` (`kind:"rename"`), and parity is asserted
  on `id` + `name` across the two decoders. A struct-tag rename on
  either side leaves one decoder's typed field empty and trips the
  test. Closes the gap CLAUDE.md "Wire types" mandates ("add an e2e
  assertion under `tests/e2e/` so drift fails CI"). Closes #902.
  (2026-05-08T09:37Z)
