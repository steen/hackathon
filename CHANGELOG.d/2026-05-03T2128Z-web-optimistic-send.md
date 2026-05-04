### web — optimistic render on send

The composer no longer blocks on the round-trip. `useMessages.send()` appends a synthetic `pending-<uuid>` entry the instant a message is submitted; when the server's WebSocket frame arrives carrying the persisted row (matching sender + body, preferring an in-window `created_at`), the hook swaps the pending entry for the server row in place — no double-render. If the REST `POST` rejects, the pending entry is marked `failed` with a Retry affordance instead of being silently dropped.

Pending rows render at reduced opacity with no timestamp; failed rows get a "Failed to send" badge and a Retry button that re-submits the original body.

Closes #125.
