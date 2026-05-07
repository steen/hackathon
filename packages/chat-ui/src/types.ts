// Public types of @hackathon/chat-ui. The package owns these so consumers
// (apps/web hooks, future apps) share one definition.

export type ConnectionStatus =
  | "idle"
  | "connecting"
  | "open"
  | "closed"
  | "reconnecting";
