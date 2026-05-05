### packages/go-client — typed ULID identifiers

Server-issued IDs on `User`, `Channel`, `Message`, and `ListMessagesOptions.Before` now carry a `goclient.ULID` type instead of a bare `string`. The new type exposes `Valid()` for client-side spec checks (26-char Crockford-base32) and provides `MarshalJSON`/`UnmarshalJSON` so the wire format stays a plain string — no server change is required, and existing JSON payloads round-trip byte-for-byte.

`Valid()` does not enforce the spec's "first character ≤ 7" timestamp ceiling; that lives server-side and a stricter client check would create a deploy-order trap.

Closes #600.
