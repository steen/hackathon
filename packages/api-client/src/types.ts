// Wire types — keep in sync with packages/go-client/{auth,channels,messages,users,ws,client,dms}.go.
// When adding a JSON field here, mirror it in Go and add an e2e assertion.

export interface User {
  id: string;
  username: string;
  // Phase-10 identity pubkeys. Optional under the L26 "optional-first"
  // wire-coordination rule: TS may declare these before the server populates
  // them (#4 lands the server side). base64 of raw 32 bytes each — see
  // specs/plans/phase-10/encryption.md "User wire-type extensions" + L2.
  box_pubkey?: string;
  sign_pubkey?: string;
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
  // Phase-10 public-channel flag. Optional and unset on the wire until #6
  // populates it; see decision-log L24 ("is_public" migration + seed).
  // Immutable after channel creation per L15.
  is_public?: boolean;
}

// MessageEnvelope is the encrypted-message wire shape from Phase 10
// (decision-log L21, specs/plans/phase-10/encryption.md). Renamed from
// "Envelope" to avoid colliding with the response-wrapper Envelope<T>
// already exported below; the JSON wire field is still `envelope`.
//
// cipher_suite = 0x01 is the only suite in v1 (naclbox-v1 — decision §3).
// signature is Ed25519 over the snakd-msg-v1:{channel,dm}: scope from L21.
// client_created_at is signed; the parent `created_at` is server-stamped
// and unsigned (display-only).
export interface MessageEnvelope {
  cipher_suite: number;
  key_generation_id: number;
  nonce: string;
  ciphertext: string;
  sender_sign_pubkey: string;
  signature: string;
  client_created_at: string;
}

// WrapEntry is the per-recipient root-key wrap that travels on every
// wrap-carrying endpoint (decision-log L5 + §7). recipient_user_id may be
// omitted when the wrap-list singularity already pins the recipient
// (e.g. the single root_key_wrap on POST /api/channels/{id}/members).
// wrapped_key is base64 of crypto_box ciphertext (48 bytes after MAC).
// nonce is base64 of 24 random bytes (XSalsa20). sender_box_pubkey is
// base64 of 32 raw bytes — the wrapper's box_pubkey at wrap time.
export interface WrapEntry {
  recipient_user_id?: string;
  wrapped_key: string;
  sender_box_pubkey: string;
  nonce: string;
}

// MembershipBlock is the inviter-signed channel-membership row from
// decision-log §10 + L22. Travels on POST /api/channels,
// POST /api/channels/{id}/members, the replay-wrap endpoint, and as the
// `membership` field of every entry in the wraps-needed response.
//
// inviter_signature is Ed25519 over the snakd-mship-v1: scope. It is null
// only for the public-channel server-auto-add carve-out (R1.2 residual,
// see security.md); private-channel rows must carry a non-null signature
// (L33 application-level enforcement in repo/channel_members.go).
export interface MembershipBlock {
  inviter_user_id: string;
  inviter_sign_pubkey: string;
  invitee_box_pubkey: string;
  invitee_sign_pubkey: string;
  added_at: string;
  inviter_signature: string | null;
}

export interface Message {
  id: string;
  channel_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
  // Phase-10 encrypted-message envelope (L21). Optional in this PR per the
  // L26 optional-first rule; #983 narrows it to required once every consumer
  // of `.body` is migrated to decrypt-from-envelope (Wave 5 / E).
  envelope?: MessageEnvelope;
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
  // Phase-10 encrypted-message envelope (L21). Optional in this PR per the
  // L26 optional-first rule; #983 narrows it to required once every consumer
  // of `.body` is migrated.
  envelope?: MessageEnvelope;
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

// ChannelEventKind carries the Phase-8 "create"/"rename" frames plus the
// Phase-10 extensions from decision-log L9 + L29:
//   - members_changed: broadcast after POST/DELETE on /channels/{id}/members
//     so recipients can race to rotate or compute lazy-wrap fill-ins.
//   - key_received: routed to the receiving user's user:<viewer> topic when
//     their wrap is filled in or rotated (closes the lazy-wrap loop).
//   - wrap_failed: routed to the user's own user:<viewer> topic when their
//     local crypto_box_open on a wrap fails — recovery is replay-wrap (L29
//     + L35).
export type ChannelEventKind =
  | "create"
  | "rename"
  | "members_changed"
  | "key_received"
  | "wrap_failed";

export type ChannelEventData =
  | { kind: "create"; channel: Channel }
  | { kind: "rename"; channel: Channel }
  | {
      kind: "members_changed";
      channel_id: string;
      current_generation_id: number;
      members_at_rotation: User[];
    }
  | { kind: "key_received"; channel_id: string; generation_id: number }
  | { kind: "wrap_failed"; channel_id: string; generation_id: number };

export interface ChannelEvent {
  type: "channel";
  data: ChannelEventData;
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
