package main

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"hackathon/apps/cli/cmd"
)

func TestStripServerFlagSeparate(t *testing.T) {
	server, rest, err := stripServerFlag([]string{"--server", "http://x", "channels"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if server != "http://x" {
		t.Errorf("server = %q, want http://x", server)
	}
	if !reflect.DeepEqual(rest, []string{"channels"}) {
		t.Errorf("rest = %v, want [channels]", rest)
	}
}

func TestStripServerFlagEqualsForm(t *testing.T) {
	server, rest, err := stripServerFlag([]string{"--server=http://y", "send", "ch", "msg"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if server != "http://y" {
		t.Errorf("server = %q, want http://y", server)
	}
	if !reflect.DeepEqual(rest, []string{"send", "ch", "msg"}) {
		t.Errorf("rest = %v, want [send ch msg]", rest)
	}
}

func TestStripServerFlagMissingValue(t *testing.T) {
	if _, _, err := stripServerFlag([]string{"--server"}); err == nil {
		t.Fatal("expected error when --server has no value")
	}
}

func TestStripServerFlagAbsent(t *testing.T) {
	server, rest, err := stripServerFlag([]string{"channels"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if server != "" {
		t.Errorf("server = %q, want empty", server)
	}
	if !reflect.DeepEqual(rest, []string{"channels"}) {
		t.Errorf("rest = %v, want [channels]", rest)
	}
}

func TestIsTopLevelHelp(t *testing.T) {
	for _, tok := range []string{"--help", "-h"} {
		if !isTopLevelHelp(tok) {
			t.Errorf("isTopLevelHelp(%q) = false, want true", tok)
		}
	}
	// `help` (the subcommand) is dispatched, not short-circuited, so it
	// reaches Dispatch's "help" case alongside the other subcommands.
	for _, tok := range []string{"", "help", "send", "--server", "register", "-help"} {
		if isTopLevelHelp(tok) {
			t.Errorf("isTopLevelHelp(%q) = true, want false", tok)
		}
	}
}

// The bare `help` subcommand routes through Dispatch and must produce
// the same byte stream as a direct WriteHelp call. With cmd.Help gone,
// this asserts the dispatcher's "help" case stays wired to WriteHelp.
func TestDispatchHelpMatchesWriteHelp(t *testing.T) {
	var direct bytes.Buffer
	if err := cmd.WriteHelp(&direct); err != nil {
		t.Fatalf("WriteHelp: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := &cmd.Env{
		Stdin:  &bytes.Buffer{},
		Stdout: stdout,
		Stderr: stderr,
	}
	if err := Dispatch(context.Background(), env, []string{"help"}); err != nil {
		t.Fatalf("Dispatch help: %v", err)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want empty", got)
	}
	if got, want := stdout.String(), direct.String(); got != want {
		t.Errorf("Dispatch help output differs from WriteHelp\n--- direct ---\n%s\n--- dispatch ---\n%s", want, got)
	}
}

func TestIsTopLevelVersion(t *testing.T) {
	for _, tok := range []string{"--version", "-v"} {
		if !isTopLevelVersion(tok) {
			t.Errorf("isTopLevelVersion(%q) = false, want true", tok)
		}
	}
	// `version` (the subcommand) is dispatched, not short-circuited;
	// `-V`, `--ver` and friends are not aliases.
	for _, tok := range []string{"", "version", "-V", "--ver", "send", "help"} {
		if isTopLevelVersion(tok) {
			t.Errorf("isTopLevelVersion(%q) = true, want false", tok)
		}
	}
}
