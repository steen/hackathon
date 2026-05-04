package auth

import (
	"sort"
	"testing"
	"time"
)

// SEC-4 / SEC-3: a percentile-based timing assertion for the constant-time
// login failure path. The existing mean-ratio test (see
// TestAuthenticateLoginConstantTimeWithinTolerance) catches gross
// regressions but a single slow GC pause can swing the mean far enough
// to either flake or mask a real leak. Asserting against the median
// (robust to outliers) plus the fast-end p10 (the value an attacker
// would actually exploit in a remote timing attack) is more stable
// against scheduler jitter and stronger against the realistic threat
// model than a single mean-ratio.
//
// This test calls AuthenticateLogin directly. The IP rate limiter that
// blocks a 100-sample E2E timing test (see PR #185) lives in HTTP
// middleware; the auth package has no such gate, so the package
// boundary is the right place for this assertion.
//
// Why no p95: under `go test ./...` the tail is dominated by scheduler
// noise from sibling packages running bcrypt-heavy work in parallel,
// not by the auth code. A noisy p95 doesn't tell you about timing
// leakage; the median and the lower tail do.
func TestAuthenticateLoginTimingPercentileEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("timing percentile test skipped under -short (bcrypt is slow)")
	}

	lookup, _ := newKnownUserLookup(t, "correct-horse-battery")

	// Warm-up: prime the bcrypt code paths and any caches so the first
	// few measured samples aren't outliers from cold starts.
	const warmups = 3
	for i := 0; i < warmups; i++ {
		_, _ = AuthenticateLogin(lookup, "alice", "wrong-password-here")
		_, _ = AuthenticateLogin(lookup, "nobody", "anything-at-all")
	}

	// Sample size is a tradeoff: bcrypt at cost 10 takes ~50-100ms per
	// round, so 50 samples per arm = 100 rounds = ~5-10s wall-clock.
	// Large enough that p10 and p50 sit on real observations (samples
	// 5 and 25), small enough to stay well under the default go-test
	// timeout. We interleave the two arms so that scheduler load drift
	// over the run hits both arms similarly.
	const samples = 50

	wrongRaw := make([]time.Duration, 0, samples)
	unknownRaw := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		s1 := time.Now()
		_, _ = AuthenticateLogin(lookup, "alice", "wrong-password-here")
		wrongRaw = append(wrongRaw, time.Since(s1))

		s2 := time.Now()
		_, _ = AuthenticateLogin(lookup, "nobody", "anything-at-all")
		unknownRaw = append(unknownRaw, time.Since(s2))
	}
	sort.Slice(wrongRaw, func(i, j int) bool { return wrongRaw[i] < wrongRaw[j] })
	sort.Slice(unknownRaw, func(i, j int) bool { return unknownRaw[i] < unknownRaw[j] })

	wrongP10, wrongP50 := percentile(wrongRaw, 10), percentile(wrongRaw, 50)
	unknownP10, unknownP50 := percentile(unknownRaw, 10), percentile(unknownRaw, 50)

	// Sanity floor: bcrypt at cost 10 is in the multi-millisecond range
	// on every machine we run on; if either fast-end percentile is
	// suspiciously fast, the dummy-hash path is probably broken.
	const floor = 5 * time.Millisecond
	if wrongP10 < floor || unknownP10 < floor {
		t.Fatalf("suspiciously fast bcrypt (dummy-hash path likely skipped): wrongP10=%v unknownP10=%v",
			wrongP10, unknownP10)
	}

	// Tolerance bounds.
	//
	// Both bands are 50% relative skew. The headline regression we
	// must catch is "unknown-user path skips the dummy bcrypt
	// comparison": that drops the unknown arm by a full bcrypt round
	// (~50-80ms vs ~60ms baseline), which is a relative skew near 1.0
	// — well outside the 0.5 band on both p10 and p50. A subtler
	// regression (lower-cost dummy hash) is also caught: cost 4 vs
	// cost 10 differs by ~15x, far outside the band. Conversely, 50%
	// is loose enough that scheduler jitter under parallel test load
	// stays inside the band (observed p50 skew on this machine: 0.00).
	const tolerance = 0.50

	p10Skew := relativeSkew(unknownP10, wrongP10)
	p50Skew := relativeSkew(unknownP50, wrongP50)

	t.Logf("SEC-4 timing percentiles: wrong p10=%v p50=%v | unknown p10=%v p50=%v | p10 skew=%.2f p50 skew=%.2f",
		wrongP10, wrongP50, unknownP10, unknownP50, p10Skew, p50Skew)

	if p10Skew > tolerance {
		t.Fatalf("SEC-4: p10 timing skew %.2f exceeds tolerance %.2f (wrong=%v unknown=%v)",
			p10Skew, tolerance, wrongP10, unknownP10)
	}
	if p50Skew > tolerance {
		t.Fatalf("SEC-4: p50 timing skew %.2f exceeds tolerance %.2f (wrong=%v unknown=%v)",
			p50Skew, tolerance, wrongP50, unknownP50)
	}
}

// percentile returns the p-th percentile of a pre-sorted slice using the
// nearest-rank method (no interpolation). p is 0..100. For n=50, p=10
// gives rank 5 (index 4) and p=50 gives rank 25 (index 24); both are
// real samples, matching the test's "percentile sits on an observation"
// assumption.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	// nearest-rank: ceil(p/100 * n), clamped to [1, n], then -1 for
	// 0-based indexing.
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// relativeSkew returns |a - b| / b, the symmetric-friendly relative
// difference used by the tolerance checks. b is the baseline (the
// known-user / wrong-password arm); a tiny b would blow up the ratio,
// but the floor sanity check above guarantees b >= 5ms in practice.
func relativeSkew(a, b time.Duration) float64 {
	if b == 0 {
		return 0
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return float64(d) / float64(b)
}
