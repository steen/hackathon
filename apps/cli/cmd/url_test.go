package cmd

import (
	"regexp"
	"testing"
)

func TestAC_0_3_URLFlagBeatsEnvAndDefault(t *testing.T) {
	t.Setenv("CHAT_SERVER", "ws://env.example/ws")
	got := ResolveURL("ws://flag.example/ws")
	if want := "ws://flag.example/ws"; got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestAC_0_3_EnvBeatsDefaultWhenFlagAbsent(t *testing.T) {
	t.Setenv("CHAT_SERVER", "ws://env.example/ws")
	got := ResolveURL("")
	if want := "ws://env.example/ws"; got != want {
		t.Errorf("ResolveURL = %q, want %q", got, want)
	}
}

func TestAC_0_3_DefaultUsedWhenNeitherFlagNorEnvSet(t *testing.T) {
	t.Setenv("CHAT_SERVER", "")
	got := ResolveURL("")
	re := regexp.MustCompile(`^ws://localhost:\d+/ws$`)
	if !re.MatchString(got) {
		t.Errorf("ResolveURL = %q, want match for ws://localhost:<port>/ws", got)
	}
}
