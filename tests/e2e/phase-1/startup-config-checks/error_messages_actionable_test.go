package startup_config_checks_e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// AC-5: All failure modes print a clear, actionable error to stderr and
// exit non-zero.
//
// This is the cross-cutting umbrella over AC-1..AC-4. Each row below is
// one failure mode; for every row the test asserts:
//
//  1. exit code is non-zero (refusal),
//  2. stderr (not stdout) is non-empty,
//  3. stderr names the env var or condition the operator must fix,
//  4. stderr does NOT leak a Go panic stack ("goroutine "), an internal
//     package path ("apps/server/internal/"), or a runtime trace
//     ("runtime/") — "actionable" means user-readable, not
//     developer-readable.
//
// The matrix is a snapshot of the failure modes called out by the spec
// AC-1..AC-4 plus the validators in apps/server/internal/config/
// config.go (empty/short/non-ASCII/repeated/low-entropy/denylisted JWT,
// missing invite, malformed bind, non-loopback bind without override).
// New failure modes added to config.Validate should be added here too.
//
// tryStartServer (harness_test.go) captures stdout+stderr in one buffer
// and is good enough for AC-1..AC-4's individual assertions but cannot
// distinguish "stderr" from "stdout". This file therefore uses a local
// tryStartServerSeparated helper that captures the two streams
// separately so the AC's literal "to stderr" claim can be verified.
func TestAC5_AllFailureModesEmitActionableStderrAndExitNonZero(t *testing.T) {
	t.Parallel()

	type failureCase struct {
		name string
		// envOverrides is layered on top of a baseline-valid env.
		// A value of "" means "set to empty string"; to OMIT a key
		// entirely (the AC-4 "unset" condition) put the key in
		// envOmit instead.
		envOverrides map[string]string
		envOmit      []string
		// mustContain are substrings every captured stderr must
		// include. They name the offending env var or recovery path
		// so the operator knows what to fix.
		mustContain []string
	}

	bin := buildServerBinary(t)

	cases := []failureCase{
		{
			name:         "jwt_secret_empty",
			envOverrides: map[string]string{"CHAT_JWT_SECRET": ""},
			mustContain:  []string{"CHAT_JWT_SECRET"},
		},
		{
			name:         "jwt_secret_too_short",
			envOverrides: map[string]string{"CHAT_JWT_SECRET": "short"},
			mustContain:  []string{"CHAT_JWT_SECRET"},
		},
		{
			name: "jwt_secret_denylisted",
			// 32 chars to clear the length floor; "change-me"
			// prefix + repeated padding char hits isDenylisted via
			// allSameAfter (config.go:215,225).
			envOverrides: map[string]string{"CHAT_JWT_SECRET": "change-meaaaaaaaaaaaaaaaaaaaaaaa"},
			mustContain:  []string{"CHAT_JWT_SECRET"},
		},
		{
			name: "jwt_secret_low_entropy",
			// 32 chars, only two distinct bytes — clears length but
			// trips isLowEntropy (config.go:198).
			envOverrides: map[string]string{"CHAT_JWT_SECRET": "abababababababababababababababab"},
			mustContain:  []string{"CHAT_JWT_SECRET"},
		},
		{
			name:         "invite_code_unset",
			envOverrides: map[string]string{},
			envOmit:      []string{"CHAT_INVITE_CODE"},
			mustContain:  []string{"CHAT_INVITE_CODE"},
		},
		{
			name:         "invite_code_empty_string",
			envOverrides: map[string]string{"CHAT_INVITE_CODE": ""},
			mustContain:  []string{"CHAT_INVITE_CODE"},
		},
		{
			name:         "bind_addr_malformed",
			envOverrides: map[string]string{"CHAT_LISTEN_ADDR": "not-a-host-port"},
			mustContain:  []string{"CHAT_LISTEN_ADDR"},
		},
		{
			name: "bind_addr_public_without_override",
			// 0.0.0.0 host without CHAT_ALLOW_PUBLIC_BIND=1: the
			// error must name both the offending var AND the
			// recovery override so the operator knows the path.
			envOverrides: map[string]string{"CHAT_LISTEN_ADDR": "0.0.0.0:8080"},
			mustContain:  []string{"CHAT_LISTEN_ADDR", "CHAT_ALLOW_PUBLIC_BIND"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := baselineValidEnv(t)
			for k, v := range tc.envOverrides {
				env[k] = v
			}
			for _, k := range tc.envOmit {
				delete(env, k)
			}

			exit, stdout, stderr := tryStartServerSeparated(t, bin, env)

			if exit == 0 {
				t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout, stderr)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("expected non-empty stderr; stdout=%q", stdout)
			}
			for _, want := range tc.mustContain {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q; stderr=\n%s", want, stderr)
				}
			}
			// "Actionable" means user-readable. A goroutine dump,
			// an internal package path, or a runtime/ trace
			// indicates the failure escaped as a panic instead of
			// a clean validation error.
			leakSentinels := []string{
				"goroutine ",
				"apps/server/internal/",
				"runtime/",
			}
			for _, sentinel := range leakSentinels {
				if strings.Contains(stderr, sentinel) {
					t.Errorf("stderr leaks developer-only material %q; stderr=\n%s", sentinel, stderr)
				}
			}
		})
	}
}

// baselineValidEnv returns an env map that — on its own — would let the
// server boot. Each table row above mutates exactly the fields under
// test so the failing condition is unambiguous.
//
// The map intentionally does NOT inherit the parent process env: an
// inherited CHAT_INVITE_CODE would mask the unset case.
func baselineValidEnv(t *testing.T) map[string]string {
	t.Helper()
	port := freePort(t)
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
	return map[string]string{
		"CHAT_JWT_SECRET":  randomSecret(t, 32),
		"CHAT_INVITE_CODE": randomSecret(t, 8),
		"CHAT_DB_PATH":     dbPath,
		"CHAT_LISTEN_ADDR": fmt.Sprintf("127.0.0.1:%d", port),
	}
}

// tryStartServerSeparated runs the built binary synchronously with the
// supplied env and a hard 10-second wall clock, capturing stdout and
// stderr into separate buffers so AC-5 can assert that the actionable
// error is on stderr specifically (not bundled into stdout).
//
// Returns (-1, "", "") only via t.Fatalf — actual returns always come
// from cmd.Run completing or an *exec.ExitError carrying the code.
func tryStartServerSeparated(t *testing.T, bin string, env map[string]string) (exitCode int, stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = envSlice(env)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err == nil {
		return 0, stdout, stderr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout, stderr
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("tryStartServerSeparated: server did not exit within 10s; stdout=%q stderr=%q", stdout, stderr)
	}
	t.Fatalf("tryStartServerSeparated: unexpected error %v; stdout=%q stderr=%q", err, stdout, stderr)
	return -1, stdout, stderr
}
