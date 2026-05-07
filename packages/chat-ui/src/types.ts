// Public types of @hackathon/chat-ui. The package owns these so consumers
// (apps/web hooks, future apps) share one definition.

export type ConnectionStatus = "idle" | "connecting" | "open" | "closed" | "reconnecting";

export type MessageStatus = "pending" | "failed";

/**
 * Shape consumed by `MessageList`. Mirrors `apps/web/src/hooks/useMessages.ts`'s
 * `MessageView`, which extends the api-client `Message` with optimistic-send
 * fields. Defined structurally here so chat-ui doesn't depend on api-client.
 */
export interface ChatMessage {
  id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
  status?: MessageStatus;
  failureReason?: string;
}

/**
 * Maximum message body size, bytes. Mirrors the server's
 * `MaxMessageBodyBytes` in `apps/server/internal/http/messages_handlers.go`
 * (the server measures bytes after TrimSpace; the client uses this for the
 * composer's over-cap guard). Single source of truth so consumers don't
 * drift from the server-side limit.
 */
export const MESSAGE_MAX_BYTES = 4 * 1024;
