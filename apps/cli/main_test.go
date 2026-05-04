package main

import (
	"reflect"
	"testing"
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
	for _, tok := range []string{"help", "--help", "-h"} {
		if !isTopLevelHelp(tok) {
			t.Errorf("isTopLevelHelp(%q) = false, want true", tok)
		}
	}
	for _, tok := range []string{"", "send", "--server", "register", "-help"} {
		if isTopLevelHelp(tok) {
			t.Errorf("isTopLevelHelp(%q) = true, want false", tok)
		}
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
