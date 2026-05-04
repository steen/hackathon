package startup_config_checks_e2e_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// AC-3: If the configured bind address is non-loopback, the server
// refuses to start unless CHAT_ALLOW_PUBLIC_BIND=1 is set.
//
// Black-box: boot the production apps/server binary against a
// non-loopback CHAT_LISTEN_ADDR and assert
//
//  1. without CHAT_ALLOW_PUBLIC_BIND, the process exits non-zero and
//     names both CHAT_LISTEN_ADDR (so the operator sees what's wrong)
//     and CHAT_ALLOW_PUBLIC_BIND (so the operator sees the recovery
//     env var),
//  2. with CHAT_ALLOW_PUBLIC_BIND=1 set, the same non-loopback address
//     boots cleanly,
//  3. a loopback address with no override still boots — the guard
//     must not block the default safe configuration.
func TestAC3_ServerRefusesPublicBindWithoutOverride(t *testing.T) {
	t.Parallel()

	t.Run("public_bind_rejected_without_override", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_INVITE_CODE": randomSecret(t, 8),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("0.0.0.0:%d", port),
			"CHAT_SERVER_PORT": fmt.Sprintf("%d", port),
			// CHAT_ALLOW_PUBLIC_BIND deliberately absent.
		}

		exit, output := tryStartServer(t, env)
		if exit == 0 {
			t.Fatalf("expected non-zero exit for public bind without override, got 0; output:\n%s", output)
		}
		if !strings.Contains(output, "CHAT_LISTEN_ADDR") {
			t.Errorf("expected output to name CHAT_LISTEN_ADDR; got:\n%s", output)
		}
		if !strings.Contains(output, "CHAT_ALLOW_PUBLIC_BIND") {
			t.Errorf("expected output to name CHAT_ALLOW_PUBLIC_BIND so operator sees the recovery path; got:\n%s", output)
		}
	})

	t.Run("public_bind_allowed_with_override", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":        randomSecret(t, 32),
			"CHAT_INVITE_CODE":       randomSecret(t, 8),
			"CHAT_DB_PATH":           dbPath,
			"CHAT_LISTEN_ADDR":       fmt.Sprintf("0.0.0.0:%d", port),
			"CHAT_SERVER_PORT":       fmt.Sprintf("%d", port),
			"CHAT_ALLOW_PUBLIC_BIND": "1",
		}

		successStartServer(t, env, port)
	})

	t.Run("loopback_bind_boots_without_override", func(t *testing.T) {
		t.Parallel()

		port := freePort(t)
		dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
		env := map[string]string{
			"CHAT_JWT_SECRET":  randomSecret(t, 32),
			"CHAT_INVITE_CODE": randomSecret(t, 8),
			"CHAT_DB_PATH":     dbPath,
			"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
			"CHAT_SERVER_PORT": fmt.Sprintf("%d", port),
		}

		successStartServer(t, env, port)
	})
}
