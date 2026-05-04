// AC-3: A constant-time login path: when a user does not exist, the
// code still performs a bcrypt comparison against a dummy hash so
// timing does not leak username existence.
//
// The contract is observable from the wire as response latency: a real
// bcrypt compare at cost=10 takes tens to hundreds of milliseconds, so
// if an unknown-user lookup short-circuited before the dummy compare,
// `T_unknown` would collapse to ~1ms — orders of magnitude below
// `T_known_wrong`. We assert the two latencies are within the same
// ballpark, and as a sanity check that response bytes are identical
// (PRD §9 SEC-4 — covered separately in auth-endpoints AC-2 but
// re-asserted here so a regression that decoupled timing from body is
// caught at this AC's first failure).
//
// Sample-count caveat: the findings doc sketched 50 iterations per
// arm, but /api/auth/login from 127.0.0.1 is rate-limited to 10
// attempts per 5 minutes (apps/server/internal/ratelimit/iplimit.go
// LoginIPConfig). The per-username UserLimiter caps another arm at 2
// failures before it gates with a 500ms+ retry-after (LoginUserConfig
// GraceFailures=2). We therefore use 4 distinct registered users +
// 4 distinct unknown usernames, one wrong-password attempt each — 8
// of the 10 IP tokens, and one failure per username so UserLimiter
// never trips. Bcrypt's order-of-magnitude latency makes N=4 enough
// to catch a missing dummy-compare regression: we assert the median
// unknown-user latency is at least half the median known-user latency
// (i.e. unknown is not 10x faster). A real timing-attack tolerance
// (the 30% delta the findings doc mentions) needs N≫10 and is not
// reachable from a single client IP without changing the server's
// rate-limit contract.

package auth_internals_e2e_test

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"
)

// constantTimeSamples is the per-arm iteration count. See the file
// header for why this is small. Each iteration uses a distinct
// username so the per-username UserLimiter records exactly one
// failure (within GraceFailures=2 — never gated).
const constantTimeSamples = 4

// minRatio is the floor for medianUnknown / medianKnown. A correctly
// implemented dummy-compare path keeps the ratio near 1.0; a missing
// compare collapses it to ~0.01. We pick 0.5 as the boundary: noisy
// enough to absorb scheduler jitter at N=4, strict enough to fail
// fast if the dummy path is ever removed.
const minRatio = 0.5

func TestAC3_ConstantTimeLoginOnUnknownUser(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-3 timing test triggers >=8 bcrypt rounds; skipped under -short")
	}

	srv := startServer(t)

	password := randomSecret(t, 16)
	wrongPassword := randomSecret(t, 16)

	knownUsernames := make([]string, constantTimeSamples)
	for i := range knownUsernames {
		u := fmt.Sprintf("alice%d-%s", i, randomSecret(t, 4))
		register(t, srv, u, password)
		knownUsernames[i] = u
	}

	unknownUsernames := make([]string, constantTimeSamples)
	for i := range unknownUsernames {
		unknownUsernames[i] = fmt.Sprintf("ghost%d-%s", i, randomSecret(t, 4))
	}

	knownLatencies := make([]time.Duration, 0, constantTimeSamples)
	unknownLatencies := make([]time.Duration, 0, constantTimeSamples)
	var firstKnownBody, firstUnknownBody []byte

	for i := 0; i < constantTimeSamples; i++ {
		status, _, raw, dur := loginRaw(t, srv, knownUsernames[i], wrongPassword)
		if status != http.StatusUnauthorized {
			t.Fatalf("known-user wrong-password #%d: status %d (want 401); body=%s", i, status, raw)
		}
		knownLatencies = append(knownLatencies, dur)
		if i == 0 {
			firstKnownBody = raw
		}

		status, _, raw, dur = loginRaw(t, srv, unknownUsernames[i], wrongPassword)
		if status != http.StatusUnauthorized {
			t.Fatalf("unknown-user #%d: status %d (want 401); body=%s", i, status, raw)
		}
		unknownLatencies = append(unknownLatencies, dur)
		if i == 0 {
			firstUnknownBody = raw
		}
	}

	if !bytes.Equal(firstKnownBody, firstUnknownBody) {
		t.Errorf("response bodies differ between known-wrong-password and unknown-user — SEC-4 byte-identical contract broken\n known=%s\n unkn =%s",
			firstKnownBody, firstUnknownBody)
	}

	medKnown := median(knownLatencies)
	medUnknown := median(unknownLatencies)
	if medKnown <= 0 {
		t.Fatalf("median known-user latency = %v; expected positive bcrypt time", medKnown)
	}
	ratio := float64(medUnknown) / float64(medKnown)
	if ratio < minRatio {
		t.Fatalf("unknown-user median %v is only %.2fx of known-user median %v (floor %.2f); the unknown-user branch likely skipped the bcrypt dummy compare — see auth.AuthenticateLogin / auth.VerifyDummy",
			medUnknown, ratio, medKnown, minRatio)
	}
	t.Logf("AC-3 timing: median known=%v unknown=%v ratio=%.2f (samples=%d, floor=%.2f)",
		medKnown, medUnknown, ratio, constantTimeSamples, minRatio)
}

// loginRaw POSTs /api/auth/login and returns the status, decoded
// envelope, raw body bytes, and the wall-clock round-trip duration.
// Mirrors postJSON's signature plus the timing dimension. Lives in
// this file (not the harness) because no other test in this package
// needs latency yet — promote when a third call site appears.
func loginRaw(t *testing.T, srv *runningServer, username, password string) (int, envelope, []byte, time.Duration) {
	t.Helper()
	start := time.Now()
	status, env, raw := postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	return status, env, raw, time.Since(start)
}

// median returns the median of xs without mutating the caller's
// slice. xs must be non-empty (the test calls Fatalf earlier on the
// empty case so a precondition violation here is a programmer error).
func median(xs []time.Duration) time.Duration {
	cp := make([]time.Duration, len(xs))
	copy(cp, xs)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}
