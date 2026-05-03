# Feature: `apps/web` (Vite + React + TS chat page)

**Parent phase:** [Phase 2: Web UI + shared clients](../phase-2-web-ui-shared-clients.md)
**Status:** planned

## Requirements covered
- US-9 — As a non-terminal user, I want a web UI, so I don't need to install anything.

## Acceptance criteria
- `apps/web` is a Vite + React + TypeScript app that builds with `pnpm --filter web build`.
- The app provides a login screen, a register screen (gated by invite code), and a chat page with channel list + message stream + input.
- The chat page subscribes via WS and updates in real time as new messages arrive.
- On WS disconnect, the client reconnects automatically with exponential backoff (and visibly indicates connection state).
- The app consumes `packages/api-client` for all server interactions.

## Implementation steps
1. Scaffold with `pnpm create vite` (React + TS template) under `apps/web`.
2. Add `packages/api-client` as a workspace dependency.
3. Implement a small auth context that loads/persists the JWT in `localStorage`.
4. Implement routes: `/login`, `/register`, `/` (chat).
5. Implement chat page: channel list (loads via REST), selected-channel message list (loads history, then subscribes via WS for new messages), and an input box that calls `POST /api/channels/{id}/messages`.
6. Implement reconnect-on-disconnect with exponential backoff inside `WebSocketClient`'s consumer hook (or via the client's built-in reconnect).
7. Build CSS to a usable level (no design polish required in this phase).

## Test plan
- `test_web_login_form_calls_login_endpoint` — covers US-9 (login).
- `test_web_register_form_requires_invite_code` — covers US-9 / US-11 surfacing.
- `test_web_chat_page_renders_history_then_appends_live_messages` — covers US-9.
- `test_web_reconnects_after_ws_disconnect` — covers US-9 reconnect requirement.
- `test_message_with_html_tags_renders_as_text_not_dom` — covers SEC-9. Asserts a posted message containing `<script>alert(1)</script>` round-trips and renders as the literal characters in the chat DOM, not as a script element.

## Files expected to be touched or created
- `apps/web/package.json`
- `apps/web/vite.config.ts`
- `apps/web/index.html`
- `apps/web/src/main.tsx`
- `apps/web/src/routes/Login.tsx`
- `apps/web/src/routes/Register.tsx`
- `apps/web/src/routes/Chat.tsx`
- `apps/web/src/auth/AuthContext.tsx`
- `apps/web/src/hooks/useChannels.ts`
- `apps/web/src/hooks/useMessages.ts`
- `apps/web/src/**/*.test.tsx`

## Risks
- Reconnect behavior is easy to get subtly wrong (e.g., infinite backoff growth, reconnect storms on auth expiry); mitigated by testing forced-close and 401-after-reconnect paths explicitly.
