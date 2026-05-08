package http_test

import (
	"testing"

	clicmd "hackathon/apps/cli/cmd"
	serverhttp "hackathon/apps/server/internal/http"
)

// TestChannelNameRegexMatchesServer fails when the CLI and server
// channel-name regexes drift apart. They are hand-mirrored validation
// rules in two packages with no shared source, so the only thing
// keeping them aligned is human discipline. This test turns drift into
// a CI failure, per issue #890.
//
// Lives under apps/server/internal/http rather than apps/cli/cmd
// because Go forbids importing apps/server/internal/* from outside
// apps/server/. Server-side test packages are allowed to import the
// (non-internal) CLI package, so this is the cycle-clean placement.
func TestChannelNameRegexMatchesServer(t *testing.T) {
	cli := clicmd.ChannelNameRe.String()
	server := serverhttp.ChannelNameRe.String()
	if cli != server {
		t.Fatalf("channel-name regex drift detected:\n  CLI    (apps/cli/cmd/channels.go)                       = %q\n  Server (apps/server/internal/http/channels_handlers.go) = %q\nUpdate both sides together and re-run.", cli, server)
	}
}
