package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
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
}

func newHelpEnv() (*Env, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &Env{
		Stdin:  &bytes.Buffer{},
		Stdout: stdout,
		Stderr: stderr,
	}, stdout, stderr
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

// `chatd help`, `chatd --help`, `chatd -h`, and `chatd` (no args) all
// route to the same printer. Calling Help with each shape of argv must
// produce the same byte stream and a nil error.
func TestHelpAllFormsProduceIdenticalOutput(t *testing.T) {
	forms := map[string][]string{
		"no-args": nil,
		"--help":  {"--help"},
		"-h":      {"-h"},
		"help":    {"help"},
	}

	var first, firstName string
	for name, args := range forms {
		env, stdout, stderr := newHelpEnv()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := Help(ctx, env, args)
		cancel()
		if err != nil {
			t.Fatalf("%s: Help returned %v", name, err)
		}
		if got := stderr.String(); got != "" {
			t.Errorf("%s: stderr = %q, want empty", name, got)
		}
		got := stdout.String()
		if got == "" {
			t.Fatalf("%s: stdout is empty", name)
		}
		if first == "" {
			first = got
			firstName = name
			continue
		}
		if got != first {
			t.Errorf("output for %s differs from %s\n--- %s ---\n%s\n--- %s ---\n%s",
				name, firstName, firstName, first, name, got)
		}
	}
}
