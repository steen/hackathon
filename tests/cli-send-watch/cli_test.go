package cli_send_watch_test

import "testing"

// These tests anchor the AC IDs from
// specs/plans/phase-0/feature-cli-send-watch.md.
//
// They are skipped while apps/cli is a stub (only doc.go). Once
// apps/cli/main.go and the send/watch subcommands exist, remove the
// t.Skip and replace each body with a real assertion against a fake
// WebSocket server (see specs/test-analysis/phase-0/cli-send-watch.md).

func TestAC1_CliSendWatch_SendWritesPayloadAsTextFrameAndExitsZero(t *testing.T) {
	t.Skip("AC-1 cli-send-watch: deferred — apps/cli is stub; see specs/test-analysis/phase-0/cli-send-watch.md")
}

func TestAC2_CliSendWatch_WatchPrintsEveryReceivedFrameOnePerLine(t *testing.T) {
	t.Skip("AC-2 cli-send-watch: deferred — apps/cli is stub")
}

func TestAC3_CliSendWatch_UrlPrecedenceFlagOverEnvOverDefault(t *testing.T) {
	t.Skip("AC-3 cli-send-watch: deferred — apps/cli is stub")
}

func TestAC4_CliSendWatch_NoAuthorizationHeaderOnUpgrade(t *testing.T) {
	t.Skip("AC-4 cli-send-watch: deferred — apps/cli is stub")
}
