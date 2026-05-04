package rate_limits_e2e_test

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// AC-2: "Login enforces a per-username backoff: repeated failures for
// the same username progressively delay subsequent attempts and/or
// return 429 after a threshold."
//
// Behavior pinned from apps/server/internal/ratelimit/userlimit.go
// (LoginUserConfig):
//
//   - GraceFailures = 2 → failures #1 and #2 are recorded but produce a
//     zero-length gate (next attempt is permitted immediately).
//   - Step = 500ms, MaxDelay = 2s → failure #3 sets nextAt = now + 500ms;
//     #4 → +1s; #5+ capped at +2s.
//   - ResetAfter = 5min → an idle username's record is dropped on the
//     next Allow/RegisterFailure call.
//
// The handler at apps/server/internal/http/auth_handlers.go:181-201
// consults the limiter on entry, calls RegisterFailure on
// AuthenticateLogin failure, and Reset on success.
//
// Per-IP token budget for the whole TestAC2_* run on 127.0.0.1:
// LoginIPConfig.Burst = 10 attempts per 5 minutes (refill ~1 token /
// 30s — too slow to count on inside a fast test). Subtests A and B run
// from 127.0.0.1 and stay under 10 login attempts. Subtest C uses a
// fresh source IP (127.0.0.2) so its login attempts get a fresh per-IP
// bucket, isolating the per-username arm from per-IP exhaustion.
func TestAC2_PerUsernameLoginBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-2 sleeps up to ~2s for the per-username backoff window to clear; skipping under -short")
	}
	srv := startServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	const (
		alice      = "alice-ac2"
		correctPwd = "correct-horse-battery-staple"
		wrongPwd   = "definitely-not-the-password"
	)

	// Registration is part of setup, not an assertion of AC-2. Subtest B
	// needs alice in the users table so the success arm (200 +
	// Reset(alice)) actually fires; subtests A and C only need a
	// username — AuthenticateLogin uses a constant-time path that
	// returns the same generic error for "no such user" and "wrong
	// password" (auth_handlers.go:158-164), and the handler increments
	// the user limiter on either arm.
	if status, env, raw := registerRaw(t, client, srv, alice, correctPwd, srv.inviteCode); status != http.StatusCreated || env.Error != nil {
		t.Fatalf("setup: register %q failed: status=%d body=%s", alice, status, raw)
	}

	// --- Subtest A: repeated failures for one username eventually 429. -------
	t.Run("repeated failures return 429 with Retry-After", func(t *testing.T) {
		// Failures #1 and #2 fall in the grace window — they produce a
		// 0ms gate, so the immediately-following attempt is allowed.
		// Failure #3 is the first one to set a real (>0) gate, so any
		// attempt arriving inside the gate window must be rejected with
		// 429. The exact attempt that observes the 429 depends on
		// scheduler/HTTP latency on the runner: on a fast machine, the
		// 4th attempt clearly arrives within failure #3's 500ms window
		// and returns 429; on a slow runner the gate may have expired
		// by then. Loop until we see 429, then assert it happened
		// within the LoginIPConfig.Burst budget.
		for i := 1; i <= 3; i++ {
			status, _, raw := loginRaw(t, client, srv, alice, wrongPwd)
			if status != http.StatusUnauthorized {
				t.Fatalf("login attempt %d/3 (grace+first-gated-failure) status=%d, want 401; body=%s", i, status, raw)
			}
		}

		// After 3 wrong-password attempts, alice has failures=3 and
		// nextAt = (server clock at failure #3) + 500ms. Issue up to 2
		// follow-up attempts, each immediately after the previous
		// response — on a normal-speed runner the first follow-up
		// arrives well within the 500ms gate, returns 429, and the
		// loop exits. Capping at 2 keeps the per-IP budget for this
		// subtest at <=5 attempts so subtest B (3 more attempts) still
		// fits inside LoginIPConfig.Burst=10.
		var gated *http.Response
		var attempts int
		for attempts = 1; attempts <= 2; attempts++ {
			resp := postLoginRaw(t, client, srv, alice, wrongPwd)
			if resp.StatusCode == http.StatusTooManyRequests {
				gated = resp
				break
			}
			resp.Body.Close()
		}
		if gated == nil {
			t.Fatalf("never observed 429 from per-username backoff after 3 priming failures + %d follow-up attempts; LoginUserConfig pins GraceFailures=2 + Step=500ms which should gate at least one attempt within the 500ms window", attempts-1)
		}
		defer gated.Body.Close()

		// Retry-After is set by writeRateLimited
		// (apps/server/internal/http/middleware_ratelimit.go:55-64) to
		// the rounded-up retry delay. For the per-username arm that's
		// the limiter's retryAfter, which is a positive sub-second
		// value rounded up to >=1s.
		ra := gated.Header.Get("Retry-After")
		if ra == "" {
			t.Errorf("429 response missing Retry-After header; writeRateLimited contracts to set it whenever retryAfter>0")
		} else if secs, err := strconv.Atoi(ra); err != nil || secs < 1 {
			t.Errorf("Retry-After=%q, want a positive integer (seconds, RFC 7231 §7.1.3)", ra)
		}
	})

	// --- Subtest B: a successful login resets the per-username counter. ------
	t.Run("successful login resets backoff counter", func(t *testing.T) {
		// Wait for alice's gate to clear. After subtest A the failure
		// count is at least 3 (the gated 429 attempt does not increment
		// the counter — Allow returns false before RegisterFailure runs
		// — so failure_count is exactly 3 when we entered this subtest,
		// modulo any extra wrong-pw call earlier; in practice it is 3
		// or 4). The maximum gate is MaxDelay=2s, so 2.2s is enough for
		// any real wall-clock to elapse past nextAt.
		time.Sleep(2200 * time.Millisecond)

		// Successful login — Reset(alice) is called inside the handler
		// after the issued token (auth_handlers.go:199-201). Without
		// reset, the failure counter would still read >=3.
		status, env, raw := loginRaw(t, client, srv, alice, correctPwd)
		if status != http.StatusOK {
			t.Fatalf("successful login after waiting past gate: status=%d body=%s", status, raw)
		}
		if !env.OK {
			t.Errorf("successful login envelope.ok=false; body=%s", raw)
		}

		// Two more wrong-password attempts. With the counter reset to
		// 0, both fall inside GraceFailures=2 → both produce a 0-length
		// gate → both return 401, never 429. If the counter had NOT
		// been reset, the first wrong attempt would push the counter
		// past 3 and gate the next, so the second attempt would land
		// on a >=500ms gate and return 429 (we issue them
		// back-to-back). Observing two 401s in a row is therefore the
		// discriminator the AC asks for ("reset on success").
		for i := 1; i <= 2; i++ {
			status, _, raw := loginRaw(t, client, srv, alice, wrongPwd)
			if status == http.StatusTooManyRequests {
				t.Fatalf("post-success wrong-pw attempt %d/2 returned 429; counter was not reset on the prior successful login (raw=%s)", i, raw)
			}
			if status != http.StatusUnauthorized {
				t.Fatalf("post-success wrong-pw attempt %d/2 status=%d, want 401; body=%s", i, status, raw)
			}
		}
	})

	// --- Subtest C: backoff is keyed per-username, not per-IP. ---------------
	t.Run("backoff is per-username — a different username from the same IP is not gated", func(t *testing.T) {
		// Use a fresh source IP so the per-IP bucket here is fresh —
		// otherwise the previous subtests' load could push the
		// per-IP limiter to 429 and mask the per-username arm.
		ipClient, err := httpClientFromIP("127.0.0.2")
		if err != nil {
			var opErr *net.OpError
			if errors.As(err, &opErr) {
				t.Skipf("kernel refused to bind 127.0.0.2 (%v); per-username-keying arm needs a fresh source IP", opErr)
			}
			t.Skipf("cannot build 127.0.0.2 client: %v", err)
		}

		// Use fresh usernames not touched by subtests A/B so the
		// per-username failure counter starts at zero. AuthenticateLogin
		// returns the same generic error for "no such user" and "wrong
		// password" (constant-time path documented at
		// auth_handlers.go:158-164), so the handler treats an unknown
		// user as a normal login failure and increments the limiter
		// just the same.
		const (
			gated   = "carol-ac2"
			ungated = "dave-ac2"
		)

		// Trip carol's backoff. With a fresh counter, failures #1 and
		// #2 fall in the grace window (delay=0) and failure #3 sets
		// the first real (>=500ms) gate. Confirm the next attempt
		// arrives inside that gate by looping up to 2 follow-ups.
		for i := 1; i <= 3; i++ {
			status, _, raw := loginRaw(t, ipClient, srv, gated, wrongPwd)
			if status != http.StatusUnauthorized {
				t.Fatalf("priming %q failure %d/3 from 127.0.0.2: status=%d, want 401; body=%s", gated, i, status, raw)
			}
		}
		var carolGated bool
		for i := 0; i < 2; i++ {
			status, _, _ := loginRaw(t, ipClient, srv, gated, wrongPwd)
			if status == http.StatusTooManyRequests {
				carolGated = true
				break
			}
		}
		if !carolGated {
			t.Fatalf("could not trip %q's per-username gate from 127.0.0.2 within 2 follow-up attempts; subtest C cannot prove per-username keying without a confirmed gate", gated)
		}

		// Discriminator: dave has zero failures recorded, so his first
		// wrong-pw attempt must return 401 even though carol is
		// currently gated from the same source IP. If the limiter
		// were keyed on IP alone (or shared across users), dave's
		// first attempt would inherit carol's gate and return 429.
		status, _, raw := loginRaw(t, ipClient, srv, ungated, wrongPwd)
		if status == http.StatusTooManyRequests {
			t.Fatalf("%q's first wrong-pw attempt returned 429 while %q was gated; per-username backoff must be keyed on username, not source IP (body=%s)", ungated, gated, raw)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("%q's first wrong-pw attempt status=%d, want 401; body=%s", ungated, status, raw)
		}
	})

}
