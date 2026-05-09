### Changed

- `chatd dm watch` and `chatd watch` now log `... watch: drop unparseable frame: <reason>` to stderr when an inbound `{type:"dm"}` or `{type:"message"}` envelope fails to decode, instead of silently dropping. Throttled to one line per 5s per stream session so a sustained malformed feed cannot spam stderr. The raw payload is never included (could leak DM bodies). Other event types (`channel`, `dm`, `read`) intentionally ignored by the channel watcher do not emit a warning. Closes #953.
