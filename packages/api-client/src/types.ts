export interface User {
  id: string;
  username: string;
}

export interface Channel {
  id: string;
  name: string;
  created_at: string;
}

export interface Message {
  id: string;
  channel_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export interface WSTicket {
  ticket: string;
  expires_at: string;
}

export interface ListMessagesOptions {
  before?: string;
  limit?: number;
}

export interface MessageEvent {
  type: "message";
  data: Message;
}

export interface PresenceEvent {
  type: "presence";
  data: {
    kind: "join" | "leave";
    user_id: string;
    channel_id?: string;
    at?: string;
  };
}

export interface UnknownEvent {
  type: string;
  data: unknown;
}

export type Event = MessageEvent | PresenceEvent | UnknownEvent;

export interface ErrorBody {
  code: string;
  message: string;
}

export interface Envelope<T> {
  ok: boolean;
  data: T | null;
  error: ErrorBody | null;
}
