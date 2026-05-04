### web — fix message history rendered in reverse chronological order on first load

`useMessages` now reverses the initial history page at the boundary (`setMessages([...history].reverse())`) so the rendered list reads oldest→newest with the composer directly under the most recent row. The REST contract is unchanged — `GET /api/channels/{id}/messages` still returns newest-first to match the `before` cursor — and every in-state operation (live WS append, reopen catchup merge-by-id, optimistic `pending-<uuid>` send + reconcile) is untouched.

Closes #99.
