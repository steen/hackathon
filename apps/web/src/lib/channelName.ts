// Mirror of the server-side channel-name regex
// (apps/server/internal/http/channels_handlers.go):
//   ^[a-z0-9][a-z0-9-]{0,39}$
// Single source of truth for the client-side pre-submit guard. Submit is
// gated locally so the user gets immediate feedback; the server is the
// authority and a 400 still surfaces inline if the rules drift.
export const CHANNEL_NAME_RE = /^[a-z0-9][a-z0-9-]{0,39}$/;

export const CHANNEL_NAME_HELPER_TEXT =
  "1–40 chars: lowercase letters, digits, hyphens; must start with a letter or digit";

export function isValidChannelName(name: string): boolean {
  return CHANNEL_NAME_RE.test(name);
}
