package auth_endpoints_e2e_test

import (
	"net/http"
	"testing"
)

// AC-4: POST /api/auth/logout increments the user's token_version,
// invalidating all outstanding tokens (US-12).
func TestAC4_Logout_IncrementsTokenVersion_InvalidatesOldToken(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	uid, tokenA1 := register(t, srv, username, password)

	// /me with tokenA1 → 200 (sanity).
	status, _, raw := getJSON(t, srv, "/api/auth/me", tokenA1)
	if status != http.StatusOK {
		t.Fatalf("/me with tokenA1 pre-logout: status %d body %s", status, raw)
	}

	// POST /api/auth/logout with tokenA1 → 200 envelope.
	status, env, raw := postJSON(t, srv, "/api/auth/logout", tokenA1, nil)
	if status != http.StatusOK {
		t.Fatalf("/logout: status %d body %s", status, raw)
	}
	if !env.OK || env.Error != nil {
		t.Fatalf("/logout envelope ok=%v error=%v", env.OK, env.Error)
	}

	// /me with the same tokenA1 → 401 (tv mismatch invalidates).
	status, _, raw = getJSON(t, srv, "/api/auth/me", tokenA1)
	if status != http.StatusUnauthorized {
		t.Fatalf("/me with tokenA1 post-logout: status %d, want 401; body=%s", status, raw)
	}

	// New login → tokenA2; tv must be exactly tv(A1)+1.
	tokenA2 := login(t, srv, username, password)
	if tokenA2 == tokenA1 {
		t.Fatalf("post-logout login returned the same token bytes")
	}
	tv1 := decodeJWTPayload(t, tokenA1)["tv"]
	tv2 := decodeJWTPayload(t, tokenA2)["tv"]
	tv1f, ok1 := tv1.(float64)
	tv2f, ok2 := tv2.(float64)
	if !ok1 || !ok2 {
		t.Fatalf("tv claims non-numeric: tv1=%T %v tv2=%T %v", tv1, tv1, tv2, tv2)
	}
	if int(tv2f) != int(tv1f)+1 {
		t.Fatalf("tv2 = %v, want tv1+1 = %v", int(tv2f), int(tv1f)+1)
	}

	// SQLite ground-truth: users.token_version equals tv2.
	db := openDBReadOnly(t, srv)
	var dbTV int
	if err := db.QueryRow(`SELECT token_version FROM users WHERE id = ?`, uid).Scan(&dbTV); err != nil {
		t.Fatalf("select token_version: %v", err)
	}
	if dbTV != int(tv2f) {
		t.Fatalf("DB token_version = %d, want %d (tv claim of fresh login)", dbTV, int(tv2f))
	}
}
