package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// expectedHelpTokens is the union of every subcommand name plus the
// env vars chatd consults. The help block must mention each.
var expectedHelpTokens = []string{
	"register",
	"login",
	"whoami",
	"logout",
	"channels",
	"history",
	"send",
	"watch",
	"help",
	"version",
	"--version",
	"--server",
	"CHAT_SERVER",
	"CHAT_PASSWORD",
	"CHAT_INVITE_CODE",
	"CHATD_CONFIG_DIR",
}

func TestWriteHelpListsEveryCommandAndEnvVar(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHelp(&buf); err != nil {
		t.Fatalf("WriteHelp: %v", err)
	}
	out := buf.String()
	for _, tok := range expectedHelpTokens {
		if !strings.Contains(out, tok) {
			t.Errorf("help output missing %q; got:\n%s", tok, out)
		}
	}
}
