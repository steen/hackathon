// Wire types — keep in sync with packages/go-client/channels.go.
// When adding a JSON field here, mirror it in Go and add an e2e assertion.

import type { HttpClient } from "./http.js";

// markChannelRead advances the viewer's read pointer for a channel. The
// server is advance-only (decision-log L5) — calls with a message_id at or
// before the current pointer are no-ops returning 200. Triggers a
// {type:"read"} WS frame on the viewer's user:<viewer> topic for
// cross-device sync.
export async function markChannelRead(
  http: HttpClient,
  channelId: string,
  messageId: string,
): Promise<void> {
  const path = `/api/channels/${encodeURIComponent(channelId)}/read`;
  await http.request<unknown>("POST", path, { message_id: messageId });
}
