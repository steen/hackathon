package cmd

import (
	"bytes"
	"testing"
)

// TestReadSecretReadsTwoLinesFromSameStdin guards the scripted-stdin
// regression: register prompts for Password then Invite code, login
// prompts for Username then Password. Both must succeed when stdin is
// a single pre-buffered pipe (heredoc / `<<<`). A previous version
// rebuilt bufio per call, draining the pipe on the first prompt and
// returning empty on the second.
func TestReadSecretReadsTwoLinesFromSameStdin(t *testing.T) {
	stdin := bytes.NewBufferString("first-line\nsecond-line\n")
	env := &Env{
		Stdin:  stdin,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}

	got1, err := readSecret(env, "", "Password")
	if err != nil {
		t.Fatalf("first readSecret: %v", err)
	}
	if got1 != "first-line" {
		t.Errorf("first readSecret = %q, want %q", got1, "first-line")
	}

	got2, err := readSecret(env, "", "Invite code")
	if err != nil {
		t.Fatalf("second readSecret: %v", err)
	}
	if got2 != "second-line" {
		t.Errorf("second readSecret = %q, want %q", got2, "second-line")
	}
}

// TestReadVisibleThenReadSecretReadTwoLines covers login's order
// (Username via readVisible, then Password via readSecret) sharing a
// single Env's stdin.
func TestReadVisibleThenReadSecretReadTwoLines(t *testing.T) {
	stdin := bytes.NewBufferString("alice\nhunter2\n")
	env := &Env{
		Stdin:  stdin,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}

	user, err := readVisible(env, "", "Username")
	if err != nil {
		t.Fatalf("readVisible: %v", err)
	}
	if user != "alice" {
		t.Errorf("readVisible = %q, want %q", user, "alice")
	}

	pw, err := readSecret(env, "", "Password")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("readSecret = %q, want %q", pw, "hunter2")
	}
}

// TestReadSecretReturnsValueWhenNonEmpty verifies the early-return
// path (flag/env-var supplied) still bypasses stdin entirely.
func TestReadSecretReturnsValueWhenNonEmpty(t *testing.T) {
	env := &Env{Stderr: &bytes.Buffer{}}
	got, err := readSecret(env, "preset", "Password")
	if err != nil {
		t.Fatalf("readSecret: %v", err)
	}
	if got != "preset" {
		t.Errorf("readSecret = %q, want %q", got, "preset")
	}
}
