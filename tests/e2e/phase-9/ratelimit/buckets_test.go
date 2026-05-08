// Package ratelimit_buckets_e2e_test boots the production chat-server
// binary with the Phase 9 dm-write and read-mark token-bucket env-var
// overrides set, and asserts the process listens cleanly. Defaults
// pinned by the decision log (lt -p direct-messages 3, L17):
// dm-write Burst=10/Refill=1m, read-mark Burst=50/Refill=1m. The
// CHAT_DM_WRITE_* / CHAT_READ_MARK_* env-var names are duplicated as
// string literals here because apps/server/internal/ratelimit is not
// importable from outside apps/server/ (Go internal-package rule);
// unit coverage of the override branches lives next to the
// implementation in apps/server/internal/ratelimit/config_test.go.
//
// This is a pure infra check — neither bucket is wired to an HTTP
// handler in this PR. Sub-issues G2 (read-mark on channel + DM read)
// and H (dm-write on DM messages) wire them. The boot here proves
// the server tolerates the env vars being present today, so the
// later wiring PRs cannot regress that contract.
package ratelimit_buckets_e2e_test

import (
	"net/http"
	"testing"

	"hackathon/tests/e2e/internal/testsupport"
)

// envDMWriteBurst / envDMWriteRefill / envReadMarkBurst /
// envReadMarkRefill mirror apps/server/internal/ratelimit/config.go.
// Drift is caught when the unit tests in that package fail; this
// duplication is the cost of crossing the internal-package boundary.
const (
	envDMWriteBurst   = "CHAT_DM_WRITE_BURST"
	envDMWriteRefill  = "CHAT_DM_WRITE_REFILL"
	envReadMarkBurst  = "CHAT_READ_MARK_BURST"
	envReadMarkRefill = "CHAT_READ_MARK_REFILL"
)

func TestPhase9BucketsServerBootsWithOverrides(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{
		ExtraEnv: []string{
			envDMWriteBurst + "=3",
			envDMWriteRefill + "=5s",
			envReadMarkBurst + "=7",
			envReadMarkRefill + "=2s",
		},
	})

	// One round-trip to /healthz confirms the server is past
	// wiring.Build and into the request-serving loop. If the buckets
	// caused a parse error in any future startup-time validation,
	// the port would not listen and StartServer would have already
	// failed; this extra check guards against the looser failure
	// mode where the binary listens but immediately panics on first
	// request.
	resp, err := http.Get(srv.HTTPURL + "/healthz") //nolint:noctx,gosec // test-only helper, fixed URL
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status: got %d, want 200", resp.StatusCode)
	}
}

// TestPhase9BucketsServerBootsWithDefaults boots the production
// chat-server with no Phase 9 bucket env vars set. The dm-write and
// read-mark configs must apply their PRD-pinned defaults silently
// (no error, no warning that would break startup) so production
// boots are unaffected by the new code paths until G2 + H wire them.
func TestPhase9BucketsServerBootsWithDefaults(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})

	resp, err := http.Get(srv.HTTPURL + "/healthz") //nolint:noctx,gosec // test-only helper, fixed URL
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status: got %d, want 200", resp.StatusCode)
	}
}
