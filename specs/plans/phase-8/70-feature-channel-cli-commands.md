# Feature: CLI — `chatd channels create` (verify) + `chatd channels rename`

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

`chatd channels create <name>` already exists (Phase 2). Phase 8 standardizes its output and adds a sibling rename command. PRD §7 (post-#835) prescribes:

```
chatd channels create <name>                    → <id>\t<name>
chatd channels rename <current-name> <new-name> → <id>\t<name>
```

Tab-separated `<id>\t<name>` matches the format of `chatd channels` (the list command), so a script can pipe `chatd channels create foo | cut -f1` to grab the new channel ULID without parsing JSON. Verify the existing `create` output and adjust to match if it currently differs.

The rename command uses the channel's current name (not its ULID) as the lookup key because users know names, not ULIDs. The CLI resolves name → id via `GET /api/channels` and then calls `PATCH /api/channels/{id}`.

## Goal

Add `chatd channels rename` and confirm/normalize `chatd channels create`'s output to `<id>\t<name>`. Both commands surface server errors with non-zero exit codes and a stderr message.

## Approach

1. New file `apps/cli/cmd/channels_rename.go` (or extend `channels.go` if the existing layout colocates subcommands; verify first). Wire it into the `channels` subcommand dispatcher.
2. `chatd channels rename <current-name> <new-name>`:
   - Calls `GET /api/channels` via `packages/go-client`, finds the channel whose `name == current-name` (case-sensitive, since the server stores names lowercase already).
   - If not found, exit 1 with `chatd: channel not found: <current-name>` to stderr.
   - Otherwise calls `PATCH /api/channels/{id}` with `{ "name": "<new-name>" }`.
   - On 200: print `<id>\t<name>` (the renamed channel's `id` and new `name`) to stdout, exit 0.
   - On 4xx: print the server's `error.message` to stderr, exit 1. Use the same exit code for all rejection cases — scripts can branch on stderr text or status code if needed.
3. `chatd channels create <name>`:
   - Verify the existing implementation prints `<id>\t<name>`. If not, change it.
   - On 4xx: same stderr-and-exit-1 pattern as rename.
4. No new flags. No JSON output mode in this PR (defer until a script driver actually needs it; "ship the thing in front of you").
5. Help text: extend the existing `chatd channels --help` output with both subcommands and their argument shapes.

## Acceptance criteria

- `chatd channels create my-room` prints `<id>\tmy-room\n` to stdout on success.
- `chatd channels rename my-room my-other-room` prints `<id>\tmy-other-room\n` to stdout on success; `<id>` is the same id as before the rename.
- Renaming a non-existent channel exits 1 with a stderr error containing the original name.
- Renaming a channel to a duplicate name exits 1 with the server's 409 message on stderr.
- Renaming `#general` exits 1 with the server's 403 message on stderr.
- Hitting the per-user rate limit (Phase 8 introduces `CHAT_CHANNEL_WRITE_BURST` / `CHAT_CHANNEL_WRITE_REFILL`) exits 1 with the server's 429 message on stderr.
- `chatd channels --help` lists both `create` and `rename` with their argument shape.
- `scripts/smoke.sh` is unchanged (this feature does not extend the smoke path; the dedicated smoke is the e2e test).

## Out of scope

- A `chatd channels delete` command — channels are not deletable in MVP.
- JSON output mode (`--json`).
- Renaming by ULID instead of name.
- Tab-completion of channel names.

## Pointers

- `apps/cli/cmd/` — existing subcommand layout (verify before adding files).
- `packages/go-client/channels.go` — extended in `80-feature-clients-channel-extensions.md` with a `RenameChannel(id, name)` call.
- PRD §7 — CLI table.
- PRD §11 US-13 — CLI happy-path acceptance row.
