package startup_config_checks_e2e_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// AC-1: Server refuses to start if the JWT signing secret is shorter
// than the documented minimum length.
//
// Black-box: boot the production apps/server binary with a JWT secret
// well under MinJWTSecretBytes (32) and assert
//
//  1. the process exits non-zero (refusal),
//  2. the captured output names CHAT_JWT_SECRET so the operator knows
//     which env var to fix,
//  3. the captured output names the minimum length (32) so the
//     operator knows the threshold.
//
// Then assert the positive path: a 32-byte hex secret (64 ASCII chars,
// well above the floor) with the rest of the env valid boots cleanly
// and the port becomes reachable.
func TestAC1_ServerRefusesShortJWTSecret(t *testing.T) {
	t.Parallel()

	t.Run("short_secret_rejected", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  "short", // 5 bytes < MinJWTSecretBytes (32)
			"CHAT_INVITE_CODE": randomSecret(t, 8),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
			// PATH is intentionally absent — the binary is statically
			// linked Go and does not exec subprocesses during startup
			// validation.
		}

		exit, output := tryStartServer(t, env)
		if exit == 0 {
			t.Fatalf("expected non-zero exit for short JWT secret, got 0; output:\n%s", output)
		}

		// AC-1 requires the error to name the minimum length so the
		// operator knows the threshold to clear. The implementation
		// formats it as "got N bytes, need at least 32" — assert on
		// "32" plus a length-shaped token so we don't pin to one
		// exact phrasing.
		if !strings.Contains(output, "CHAT_JWT_SECRET") {
			t.Errorf("expected output to name CHAT_JWT_SECRET; got:\n%s", output)
		}
		if !strings.Contains(output, "32") {
			t.Errorf("expected output to name minimum length 32; got:\n%s", output)
		}
		lengthSignal := strings.Contains(output, "too short") ||
			strings.Contains(output, "at least") ||
			strings.Contains(output, "minimum") ||
			strings.Contains(output, "length")
		if !lengthSignal {
			t.Errorf("expected output to signal a length-based rejection (one of: too short / at least / minimum / length); got:\n%s", output)
		}

	})

	t.Run("long_enough_secret_boots", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		// 32 random bytes -> 64 hex chars, comfortably above the 32-byte floor.
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_INVITE_CODE": randomSecret(t, 8),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
		}

		successStartServer(t, env, port)
	})
}
