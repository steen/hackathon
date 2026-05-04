package rate_limits_e2e_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// AC-1: "Login and register endpoints enforce a per-IP rate limit
// (e.g., token bucket with burst + steady-state thresholds documented
// in code)."
//
// Thresholds are pinned from apps/server/internal/ratelimit/iplimit.go:
//   - LoginIPConfig:    Burst=10, Refill=5min  (11th login from one IP -> 429)
//   - RegisterIPConfig: Burst=5,  Refill=15min (6th register from one IP -> 429)
//
// The test boots the production binary, drives /api/auth/login and
// /api/auth/register over real HTTP from one source IP, and asserts the
// burst+1 attempt returns 429. A best-effort second arm dials from
// 127.0.0.2 to confirm the per-IP bucket is keyed on source IP, not
// shared across the loopback prefix; that arm is skipped on platforms
// where the kernel refuses to bind 127.0.0.2.
func TestAC1_PerIPRateLimit_LoginAndRegister(t *testing.T) {
	srv := startServer(t)

	t.Run("login: 11th attempt from one IP returns 429", func(t *testing.T) {
		client := &http.Client{Timeout: 5 * time.Second}
		// Vary the username across attempts so the per-username login
		// backoff (LoginUserConfig: GraceFailures=2 → 429 on the 3rd
		// failure of one username, AC-2's territory) doesn't fire and
		// pre-empt the per-IP bucket we're measuring here. The IP
		// limiter still counts every attempt because it's keyed on
		// source IP, not username.
		for i := 1; i <= 10; i++ {
			user := fmt.Sprintf("ac1-login-%03d", i)
			status, _, raw := loginRaw(t, client, srv, user, "wrong-password")
			if status == http.StatusTooManyRequests {
				t.Fatalf("login attempt %d/10 got 429; LoginIPConfig burst=10 should permit the first 10 attempts (body=%s)", i, raw)
			}
			if status != http.StatusUnauthorized {
				t.Fatalf("login attempt %d/10 status=%d, want 401; body=%s", i, status, raw)
			}
		}
		status, env, raw := loginRaw(t, client, srv, "ac1-login-011", "wrong-password")
		if status != http.StatusTooManyRequests {
			t.Fatalf("11th login attempt from one IP must be 429 (LoginIPConfig burst=10); got %d body=%s", status, raw)
		}
		if env.OK || env.Error == nil || env.Error.Code == "" {
			t.Fatalf("429 envelope should have ok=false and a non-empty error.code; got ok=%v err=%+v body=%s", env.OK, env.Error, raw)
		}
	})

	t.Run("register: 6th attempt from one IP returns 429", func(t *testing.T) {
		client := &http.Client{Timeout: 5 * time.Second}
		// Use a wrong invite_code so each call exercises the IP middleware
		// without committing a row to users; the bucket counts attempts
		// regardless of handler outcome.
		const wrongInvite = "definitely-not-the-invite"
		for i := 1; i <= 5; i++ {
			status, _, raw := registerRaw(t, client, srv, "bob", "correct-horse-battery", wrongInvite)
			if status == http.StatusTooManyRequests {
				t.Fatalf("register attempt %d/5 got 429; RegisterIPConfig burst=5 should permit the first 5 attempts (body=%s)", i, raw)
			}
		}
		status, env, raw := registerRaw(t, client, srv, "bob", "correct-horse-battery", wrongInvite)
		if status != http.StatusTooManyRequests {
			t.Fatalf("6th register attempt from one IP must be 429 (RegisterIPConfig burst=5); got %d body=%s", status, raw)
		}
		if env.OK || env.Error == nil || env.Error.Code == "" {
			t.Fatalf("429 envelope should have ok=false and a non-empty error.code; got ok=%v err=%+v body=%s", env.OK, env.Error, raw)
		}
	})

	t.Run("login: a different source IP is not affected by the first IP's exhausted bucket", func(t *testing.T) {
		// The previous subtests exhausted the login bucket for 127.0.0.1.
		// A fresh source IP (127.0.0.2) must still be allowed through.
		// Many CI runners (Linux) accept any 127.0.0.0/8 source; macOS
		// requires `sudo ifconfig lo0 alias 127.0.0.2` or returns
		// EADDRNOTAVAIL. Skip on bind failure rather than fail the run.
		client, err := httpClientFromIP("127.0.0.2")
		if err != nil {
			var opErr *net.OpError
			if errors.As(err, &opErr) {
				t.Skipf("kernel refused to bind 127.0.0.2 (%v); cross-IP arm skipped", opErr)
			}
			t.Skipf("cannot build 127.0.0.2 client: %v", err)
		}
		status, _, raw := loginRaw(t, client, srv, "ac1-cross-ip-user", "wrong-password")
		if status == http.StatusTooManyRequests {
			t.Fatalf("first attempt from 127.0.0.2 returned 429; per-IP bucket must not be shared across source IPs (body=%s)", raw)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("first attempt from 127.0.0.2 status=%d, want 401; body=%s", status, raw)
		}
	})
}
