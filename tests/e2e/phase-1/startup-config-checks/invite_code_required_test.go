package startup_config_checks_e2e_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// AC-4: Server refuses to start if CHAT_INVITE_CODE is unset (since
// registration depends on it; see US-11).
//
// Black-box: boot the production apps/server binary with a deliberately
// clean env that omits CHAT_INVITE_CODE, and assert
//
//  1. the process exits non-zero (refusal),
//  2. the captured output names CHAT_INVITE_CODE so the operator knows
//     which env var to set.
//
// The empty-string variant is asserted alongside the unset case because
// os.Getenv returns "" for both and the spec's "unset" intent covers
// both shapes.
//
// envSlice in harness_test.go does not inherit from the test process,
// so an inherited CHAT_INVITE_CODE in the test runner cannot mask this
// condition.
//
// Positive control: a randomly-generated invite code with the rest of
// the env valid boots cleanly.
func TestAC4_ServerRefusesMissingInviteCode(t *testing.T) {
	t.Parallel()

	t.Run("unset_invite_code_rejected", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
			// CHAT_INVITE_CODE deliberately absent.
		}

		exit, output := tryStartServer(t, env)
		if exit == 0 {
			t.Fatalf("expected non-zero exit when CHAT_INVITE_CODE is unset, got 0; output:\n%s", output)
		}
		if !strings.Contains(output, "CHAT_INVITE_CODE") {
			t.Errorf("expected output to name CHAT_INVITE_CODE; got:\n%s", output)
		}
		requiredSignal := strings.Contains(output, "required") ||
			strings.Contains(output, "must be set") ||
			strings.Contains(output, "is not set") ||
			strings.Contains(output, "missing")
		if !requiredSignal {
			t.Errorf("expected output to signal CHAT_INVITE_CODE is required (one of: required / must be set / is not set / missing); got:\n%s", output)
		}
	})

	t.Run("empty_invite_code_rejected", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_INVITE_CODE": "",
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
		}

		exit, output := tryStartServer(t, env)
		if exit == 0 {
			t.Fatalf("expected non-zero exit when CHAT_INVITE_CODE is empty, got 0; output:\n%s", output)
		}
		if !strings.Contains(output, "CHAT_INVITE_CODE") {
			t.Errorf("expected output to name CHAT_INVITE_CODE; got:\n%s", output)
		}
	})

	t.Run("set_invite_code_boots", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_INVITE_CODE": randomSecret(t, 8),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
		}

		successStartServer(t, env, port)
	})
}
