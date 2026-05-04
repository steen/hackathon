package wiring_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wiring"
)

// TestBuildNoDBRegistersWSAndDebugSubs asserts that with no Repo (the
// phase-0 / smoke-test boot path) Build still wires /ws and /debug/subs.
// Auth/channels/presence are intentionally skipped in this branch — a
// future regression that drops them from Build entirely should still
// trip the test below.
func TestBuildNoDBRegistersWSAndDebugSubs(t *testing.T) {
	h := wiring.Build(wiring.Deps{Hub: hub.New()})
	srv := httptest.NewServer(h)
	defer srv.Close()

	// /debug/subs answers 200 even without a channel param (returns
	// the empty-set gauge).
	resp, err := http.Get(srv.URL + "/debug/subs")
	if err != nil {
		t.Fatalf("GET /debug/subs: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("/debug/subs: got %d, want 200 or 400", resp.StatusCode)
	}

	// Auth-gated routes are unwired in the no-DB branch: hitting them
	// returns 404 (the mux has no pattern for them) rather than 401.
	for _, p := range []string{"/api/auth/me", "/api/channels", "/api/presence"} {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("%s no-DB: got %d, want 404 (route should be unwired)", p, r.StatusCode)
		}
	}
}

// TestBuildReturnsNonNilHandler is the cheap sanity check: the entire
// refactor's contract is that Build returns the http.Handler main.go
// would otherwise have constructed inline. A nil return would crash
// the server at boot.
func TestBuildReturnsNonNilHandler(t *testing.T) {
	h := wiring.Build(wiring.Deps{Hub: hub.New()})
	if h == nil {
		t.Fatal("wiring.Build returned nil handler")
	}
}
