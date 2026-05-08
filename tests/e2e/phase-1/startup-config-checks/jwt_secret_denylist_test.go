package startup_config_checks_e2e_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// AC-2: Server refuses to start if the JWT signing secret matches a
// dev-default denylist (e.g., empty string, `dev`, `secret`,
// `change-me`).
//
// Black-box: boot the production apps/server binary with each
// denylisted value and assert
//
//  1. the process exits non-zero (refusal),
//  2. the captured output names CHAT_JWT_SECRET so the operator knows
//     which env var to fix.
//
// The bare values "", "dev", "secret", and "change-me" are all under
// the 32-byte length floor, so the rejection here may overlap with the
// AC-1 length check; per
// specs/test-analysis/phase-1/startup-config-checks.md the overlap is
// expected and both ACs are satisfied by the same rejection. The
// padded sub-cases (e.g. "change-me" + "e"*23) clear the length floor
// and exercise the denylist branch in
// apps/server/internal/config/config.go specifically — that branch
// returns the "matches a known dev-default value" message.
//
// Positive control: a 32-byte random hex secret boots cleanly.
func TestAC2_ServerRefusesDenylistedJWTSecret(t *testing.T) {
	t.Parallel()

	denyValues := []string{
		// Bare values from the spec. All shorter than MinJWTSecretBytes
		// (32) so they trip either the empty check, the length check,
		// or the denylist itself; what matters for AC-2 is that the
		// process refuses to start and the output names the env var.
		"",
		"dev",
		"secret",
		"change-me",

		// Padded variants that clear the 32-byte length floor and so
		// reach the denylist branch directly. The padding character
		// matches the prefix's last byte so allSameAfter() in
		// validateJWTSecret matches.
		"change-me" + strings.Repeat("e", 32),
		"secret" + strings.Repeat("t", 32),
		"dev" + strings.Repeat("v", 32),

		// Case-insensitive denylist coverage.
		"CHANGE-ME" + strings.Repeat("E", 32),
		"Secret" + strings.Repeat("t", 32),
	}

	for _, secret := range denyValues {
		secret := secret
		name := secret
		if name == "" {
			name = "empty"
		}
		t.Run("rejects/"+name, func(t *testing.T) {
			t.Parallel()

			port := freePort(t)
			dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
			env := map[string]string{
				"CHAT_JWT_SECRET":  secret,
				"CHAT_INVITE_CODE": randomSecret(t, 8),
				"CHAT_DB_PATH":     dbPath,
				"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
			}

			exit, output := tryStartServer(t, env)
			if exit == 0 {
				t.Fatalf("expected non-zero exit for denylisted JWT secret %q, got 0; output:\n%s", secret, output)
			}
			if !strings.Contains(output, "CHAT_JWT_SECRET") {
				t.Errorf("expected output to name CHAT_JWT_SECRET; got:\n%s", output)
			}
		})
	}

	t.Run("random_secret_boots", func(t *testing.T) {
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
