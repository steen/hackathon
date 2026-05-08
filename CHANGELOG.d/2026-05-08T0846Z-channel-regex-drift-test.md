### Internal

- `apps/server/internal/http`: add `TestChannelNameRegexMatchesServer` which imports the channel-name regex from both `apps/cli/cmd` and `apps/server/internal/http` and asserts `String()` equality. The two regexes are hand-mirrored validation rules with no shared source; this test turns silent drift into a CI failure. Each package now exposes its `channelNameRe` as `ChannelNameRe` (an alias to the same compiled pattern) so the test can see them. Test lives server-side because Go forbids importing `apps/server/internal/*` from `apps/cli/cmd`. Closes #890.
