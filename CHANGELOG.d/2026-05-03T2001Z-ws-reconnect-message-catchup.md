### web/useMessages — catchup on WS reopen

A message posted by another client while the WebSocket was down now appears in the reconnecting client's view without a page reload. On every WS reopen after the initial connect, `useMessages` refetches the recent message window via `GET /api/channels/{id}/messages?limit=50` and merges any unseen rows by id.

The hook still appends live frames as they arrive; catchup only runs on reopen and silently leaves state unchanged on fetch failure (avoids clobbering an otherwise-healthy connection state).

Closes #108.
