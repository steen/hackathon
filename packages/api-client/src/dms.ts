// Wire types — keep in sync with packages/go-client/{channels,messages,ws,client,dms}.go.
// When adding a JSON field here, mirror it in Go and add an e2e assertion.

import type { HttpClient } from "./http.js";
import type { Conversation, DMMessage, ListDMMessagesOptions } from "./types.js";

// createDM is idempotent find-or-create. The server returns 201 on create
// and 200 on existing (decision-log L18); both responses carry the full
// Conversation including the peer summary computed for the viewer.
export function createDM(http: HttpClient, peerUserId: string): Promise<Conversation> {
  return http.request<Conversation>("POST", "/api/dms", {
    peer_user_id: peerUserId,
  });
}

// listDMs returns every conversation the viewer participates in that has at
// least one message (decision §3 hides empty conversations). No pagination
// in v1 (L12). The server attaches peer + unread_count per row.
export async function listDMs(http: HttpClient): Promise<Conversation[]> {
  const data = await http.request<{ conversations: Conversation[] }>("GET", "/api/dms");
  return data.conversations;
}

// sendDMMessage posts a message to an existing conversation. The server
// validates membership (404 on non-participation per L8), enforces the
// 4096-byte body cap (L16), and applies the per-user dm-write rate-limit
// bucket (burst 10 / refill 1m — L17).
export function sendDMMessage(
  http: HttpClient,
  conversationId: string,
  body: string,
): Promise<DMMessage> {
  const path = `/api/dms/${encodeURIComponent(conversationId)}/messages`;
  return http.request<DMMessage>("POST", path, { body });
}

// listDMMessages returns up to opts.limit messages newest-first. The cursor
// shape mirrors GET /api/channels/{id}/messages: ?before=<msg_id> is
// exclusive, default limit=50, server-capped at 200.
export async function listDMMessages(
  http: HttpClient,
  conversationId: string,
  opts: ListDMMessagesOptions = {},
): Promise<DMMessage[]> {
  const qs = new URLSearchParams();
  if (opts.before) qs.set("before", opts.before);
  if (opts.limit !== undefined && opts.limit > 0) {
    qs.set("limit", String(opts.limit));
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  const path = `/api/dms/${encodeURIComponent(conversationId)}/messages${suffix}`;
  const data = await http.request<{ messages: DMMessage[] }>("GET", path);
  return data.messages;
}

// markDMRead advances the viewer's read pointer for the conversation. The
// server is advance-only (L5) — calls with a message_id <= the current
// pointer are no-ops returning 200. The first call materializes the
// recipient's dm_reads row (NULL until then per §11). Triggers a
// {type:"read"} WS frame on the viewer's user:<viewer> topic for
// cross-device sync.
export async function markDMRead(
  http: HttpClient,
  conversationId: string,
  messageId: string,
): Promise<void> {
  const path = `/api/dms/${encodeURIComponent(conversationId)}/read`;
  await http.request<unknown>("POST", path, { message_id: messageId });
}
