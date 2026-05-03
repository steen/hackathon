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
