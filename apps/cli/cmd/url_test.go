package cmd_test

import (
	"fmt"
	"testing"

	"github.com/jumoel/hackathon/apps/cli/cmd"
	"github.com/jumoel/hackathon/packages/go-shared/serverdefaults"
)

func TestAC3_ResolveURL_FlagWinsOverEnvAndDefault(t *testing.T) {
	got := cmd.ResolveURL("ws://flag.example/ws", "ws://env.example/ws")
	if want := "ws://flag.example/ws"; got != want {
		t.Fatalf("ResolveURL = %q, want %q", got, want)
	}
}

func TestAC3_ResolveURL_EnvWinsOverDefault(t *testing.T) {
	got := cmd.ResolveURL("", "ws://env.example/ws")
	if want := "ws://env.example/ws"; got != want {
		t.Fatalf("ResolveURL = %q, want %q", got, want)
	}
}

func TestAC3_ResolveURL_FallsBackToLocalhostDefault(t *testing.T) {
	got := cmd.ResolveURL("", "")
	want := fmt.Sprintf("ws://localhost:%d/ws", serverdefaults.Port)
	if got != want {
		t.Fatalf("ResolveURL = %q, want %q", got, want)
	}
}

func TestAC3_ResolveURL_RejectsEmptyAfterTrim(t *testing.T) {
	got := cmd.ResolveURL("   ", "")
	want := fmt.Sprintf("ws://localhost:%d/ws", serverdefaults.Port)
	if got != want {
		t.Fatalf("ResolveURL(whitespace flag) = %q, want default %q", got, want)
	}
}
