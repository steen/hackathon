### Docs

- `apps/server/internal/http/channels_handlers.go`, `apps/cli/cmd/channels.go`: fix the wrong test path in the `ChannelNameRe` alias comment (the drift test lives at `apps/server/internal/http/channels_regex_drift_test.go`, not under `apps/cli/cmd/`) and name `TestChannelNameRegexMatchesServer` in both regex-var comments so a future reader who edits one side sees the test pointer immediately. Comment-only; aliases stay pointer-equal to `channelNameRe`.
