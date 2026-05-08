# Feature: Web UI — create + rename channel

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

The web app today lists channels in a sidebar but exposes no UI for creating or renaming. Phase 8 adds both:

- A "+ New channel" affordance at the top of the channel sidebar opens the create-channel modal.
- A rename affordance on each non-`#general` channel row (icon button, hover-revealed) opens the rename-channel modal.

Both modals reuse the `<Modal>` primitive from `40-feature-modal-primitive.md` and consume the channel snapshot from `useChannels()` in `50-feature-web-shared-ws-provider.md`. Server validation rules (name shape, duplicate detection, 403 on `#general`, 429 rate limit) are mirrored in user-facing copy but the UI does NOT pre-validate against the channel list — it submits and surfaces server errors verbatim. Pre-validation goes stale fast and duplicates server logic.

## Goal

Land both modals + their entry points in the sidebar, wired through `packages/api-client` to `POST /api/channels` and `PATCH /api/channels/{id}`. After a successful submit, the modal closes and the (re-fetched / WS-broadcast-updated) channel list reflects the change.

## Approach

1. Create modal:
   - Triggered by a "+ New channel" button at the top of the channel sidebar.
   - One text input (`name`), submit button, cancel button.
   - On submit: call `apiClient.createChannel({ name })` (added in `80-feature-clients-channel-extensions.md`).
   - On 200: close modal; the WS `channel` frame from `30-feature-channel-ws-events.md` updates the sidebar via `useChannels()`. Optimistic UI is **not** required — the WS round-trip is sub-second.
   - On 400 / 409 / 429: render the error envelope's `error.message` inline in the modal; keep the modal open.
2. Rename modal:
   - Triggered by an icon button on each channel row except `#general`.
   - The button is rendered with `aria-label="Rename <channel>"` for screen-reader use; CSS hover rules surface it for sighted users.
   - Pre-fills the current name; submit calls `apiClient.renameChannel(id, { name })`.
   - On 200: close modal; the `channel` frame with `kind: "rename"` updates the sidebar entry in place.
   - On 400 / 403 / 409 / 429: render the error envelope's `error.message` inline; keep the modal open.
3. Sidebar:
   - The `#general` channel row never shows the rename button (matching the server's 403). Match by name, not by ULID, so reseeding works.
   - Pending state: while a write is in flight, the modal's submit button is disabled and the input is read-only.
4. Routing: neither modal changes the URL. Both are pure local state on the sidebar.

## Acceptance criteria

- "+ New channel" button is present at the top of the channel sidebar; clicking it opens the create modal.
- Rename icon button is present on every channel row except `#general` (selector: presence/absence per row).
- Submitting a valid name in create modal: modal closes, sidebar shows the new channel within one render cycle of the `channel` frame arriving.
- Submitting a duplicate name: modal stays open and surfaces the server's `error.message`.
- Submitting an invalid name (e.g. uppercase, too long): modal stays open and surfaces the server's 400 message.
- Hitting the per-user rate limit: modal stays open and surfaces the 429 message.
- Submitting a rename: modal closes; the renamed entry replaces the old name in place (no duplicate row).
- Renaming `#general` is impossible from the UI (button absent); a manual `PATCH` from devtools still receives the server's 403 — which is the contract — but the UI does not expose it.

## Out of scope

- Channel deletion / archive UI.
- Multi-step creation flow (icon, description, etc.).
- Drag-to-reorder channels.
- Optimistic update of the sidebar before the WS frame arrives.
- Inline-rename (clicking the channel name to edit) — modal-only in Phase 8; revisit if the modal feels heavy in practice.

## Pointers

- `apps/web/src/components/Modal.tsx` — primitive from `40-feature-modal-primitive.md`.
- `apps/web/src/ws/WsProvider.tsx` — `useChannels()` from `50-feature-web-shared-ws-provider.md`.
- `packages/api-client/src/channels.ts` (or wherever the existing channel calls live; verify before editing) — extended in `80-feature-clients-channel-extensions.md`.
- PRD §10 Channels — wire contract for status codes.
- PRD §11 US-4 / US-13 — Playwright assertions.
