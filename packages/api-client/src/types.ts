// Wire types — keep in sync with packages/go-client/{auth,channels,messages,users,ws,client,dms}.go.
// When adding a JSON field here, mirror it in Go and add an e2e assertion.

export interface User {
  id: string;
  username: string;
}

export interface Channel {
  id: string;
  name: string;
  created_at: string;
  // Phase-9 listing-additive fields. Optional under the L26 "optional-first"
  // wire-coordination rule: TS may declare these before the server populates
  // them; the e2e drift assertion lands in the server-side populator
  // (sub-issue G2). See specs/plans/phase-9/read-state.md.
  last_message_id?: string | null;
  last_message_at?: string | null;
  unread_count?: number;
  last_read_message_id?: string | null;
}

export interface Message {
  id: string;
  channel_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
}

// Conversation mirrors the wire shape from specs/plans/phase-9/dms.md.
// peer is the OTHER participant relative to the requesting viewer; the
// server computes it per-request. user_a_id/user_b_id are stored in
// canonical order (user_a_id < user_b_id) per decision-log L2.
export interface Conversation {
  id: string;
  user_a_id: string;
  user_b_id: string;
  last_message_id: string | null;
  last_message_at: string | null;
  created_at: string;
  peer: User;
  unread_count: number;
}

// DMMessage is immutable on the wire (decision-log L9 — no edit/delete in
// v1). body shares the 4096-byte cap with channel messages
// (MaxMessageBodyBytes — L16).
export interface DMMessage {
  id: string;
  conversation_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
}

export interface ListDMMessagesOptions {
  before?: string;
  limit?: number;
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

export type ChannelEventKind = "create" | "rename";

export interface ChannelEvent {
  type: "channel";
  data: {
    kind: ChannelEventKind;
    channel: Channel;
  };
}

// DMEvent is self-sufficient on first contact (decision §8): the embedded
// conversation block lets the recipient render a new sidebar entry without a
// GET /api/dms round-trip. Field name `dm_message` matches
// specs/plans/phase-9/dms.md.
export interface DMEvent {
  type: "dm";
  data: {
    conversation: Conversation;
    dm_message: DMMessage;
  };
}

// ReadEvent is routed only to the originating viewer's user:<viewer> topic
// (cross-device sync — decision §7, L10). target_id is the channel id when
// scope=channel and the conversation id when scope=dm. unread_count is the
// server-computed count after the advance commits.
export interface ReadEvent {
  type: "read";
  data: {
    scope: "channel" | "dm";
    target_id: string;
    last_read_message_id: string;
    unread_count: number;
  };
}

export interface UnknownEvent {
  type: string;
  data: unknown;
}

export type Event =
  | MessageEvent
  | PresenceEvent
  | ChannelEvent
  | DMEvent
  | ReadEvent
  | UnknownEvent;

export interface ErrorBody {
  code: string;
  message: string;
}

export interface Envelope<T> {
  ok: boolean;
  data: T | null;
  error: ErrorBody | null;
}
