### Added

- **Phase 10 channel membership**: explicit `channel_members` relation is now the source of truth for who can see which channel.
  - `POST /api/channels` accepts an `is_public` boolean (default false). The seeded `#general` channel is `is_public = TRUE`; every newly registered user is auto-added to every `is_public = TRUE` channel inside the same transaction as the `users` insert (decision-log §9 + R1.2 — NULL `inviter_signature` accepted only for the public-channel carve-out per L33).
  - New surface: `GET /api/channels/{id}/members`, `POST /api/channels/{id}/members`, `DELETE /api/channels/{id}/members/{user_id}`. Self-leave is allowed; #general membership is immutable per L8 and rejects both kick and self-leave with 403.
  - `GET /api/channels` filters to channels the viewer is a member of (decision-log §6 + L25). `MaterializeChannelReadsTx` joins `channel_members` on both the pre-check count and the sweep insert so the channel-reads bookkeeping no longer creates rows for non-member channels (RACE-3).
  - CLI: `chatd channels create --public`, `chatd channels {invite,kick,leave,members}`.
  - Web client: `HttpClient.{listChannelMembers,inviteChannelMember,kickChannelMember}` and an exported `ChannelMember` type.
  - Out of scope (deferred): inviter-signature crypto verify and key-wrap on invite — both land with #984.
