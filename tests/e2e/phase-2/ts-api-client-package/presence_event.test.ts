// AC-6: Once 50-feature-presence.md lands, the client must surface
// the `presence` event type (kind `join` / `leave`) through the same
// emitter; design the `Event` union with that in mind.
//
// Skipped at this SHA: presence is filed for phase-3. The Event
// union already includes a `PresenceEvent` arm — the design half of
// the AC is satisfied — but a runtime test needs the server-side
// presence broadcast to exist. Un-skip this once
// 50-feature-presence ships.

import { describe, it } from "vitest";

describe("AC-6: presence event surfaced through the emitter", () => {
  it.skip("AC-6: WebSocketClient receives presence join/leave events when a peer connects/disconnects", () => {
    // Deferred — un-skip after specs/plans/phase-3/50-feature-presence.md
    // is implemented. Two clients connect to the same channel; the
    // first asserts a 'message' event with type 'presence' and
    // data.kind ∈ {"join","leave"} fires when the second
    // joins/leaves.
  });
});
