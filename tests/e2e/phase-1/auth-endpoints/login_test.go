package auth_endpoints_e2e_test

import (
	"net/http"
	"testing"
)

// AC-2: POST /api/auth/login returns a JWT including a `tv` claim on
// success; constant-time on failure (US-2).
//
// "Constant-time on failure" is asserted only as byte-identical
// {code, message} on the unknown-user vs wrong-password arms (PRD §9
// SEC-4). Real timing-percentile checks live in auth-internals (see
// findings doc); they require either a fake clock or far more samples
// than the IP rate limiter (10/5min) lets us send to the same endpoint
// from 127.0.0.1.
func TestAC2_Login_ReturnsJWTWithTV_ByteIdenticalFailure(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	uid, _ := register(t, srv, username, password)

	// Happy path: 200 + JWT with `sub`/`tv`/`exp`.
	tok := login(t, srv, username, password)
	claims := decodeJWTPayload(t, tok)

	if got, want := claims["sub"], uid; got != want {
		t.Fatalf("JWT.sub = %v, want %v (the registered user id)", got, want)
	}
	tv, ok := claims["tv"].(float64)
	if !ok {
		t.Fatalf("JWT.tv missing or non-numeric: %T %v", claims["tv"], claims["tv"])
	}
	if int(tv) != 0 {
		t.Fatalf("JWT.tv = %v, want 0 right after register", tv)
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("JWT.exp missing or non-numeric: %T %v", claims["exp"], claims["exp"])
	}
	if exp <= 0 {
		t.Fatalf("JWT.exp = %v, want > 0", exp)
	}

	// Wrong password and unknown username must produce byte-identical
	// {code, message}. Using only one attempt per arm (the IP rate
	// limiter lets 10 logins per 5 minutes through from 127.0.0.1).
	wrongStatus, wrongEnv, wrongRaw := postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": username,
		"password": "definitely-wrong-" + randomSecret(t, 4),
	})
	if wrongStatus != http.StatusUnauthorized {
		t.Fatalf("wrong-password: status %d, want 401; body=%s", wrongStatus, wrongRaw)
	}
	if wrongEnv.OK || wrongEnv.Error == nil {
		t.Fatalf("wrong-password: envelope ok=%v error=%v", wrongEnv.OK, wrongEnv.Error)
	}

	unkStatus, unkEnv, unkRaw := postJSON(t, srv, "/api/auth/login", "", map[string]string{
		"username": "no-such-user-" + randomSecret(t, 4),
		"password": password,
	})
	if unkStatus != http.StatusUnauthorized {
		t.Fatalf("unknown-user: status %d, want 401; body=%s", unkStatus, unkRaw)
	}
	if unkEnv.OK || unkEnv.Error == nil {
		t.Fatalf("unknown-user: envelope ok=%v error=%v", unkEnv.OK, unkEnv.Error)
	}

	if wrongEnv.Error.Code != unkEnv.Error.Code {
		t.Errorf("error codes differ between unknown-user (%q) and wrong-password (%q) — SEC-4 violation",
			unkEnv.Error.Code, wrongEnv.Error.Code)
	}
	if wrongEnv.Error.Message != unkEnv.Error.Message {
		t.Errorf("error messages differ between unknown-user (%q) and wrong-password (%q) — SEC-4 violation",
			unkEnv.Error.Message, wrongEnv.Error.Message)
	}
}
