package auth_endpoints_e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// AC-3: GET /api/auth/me returns the current user when given a valid
// bearer token; 401 otherwise.
func TestAC3_Me_RequiresValidBearer(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12)
	uid, tok := register(t, srv, username, password)

	// Happy path: valid token → 200 envelope with id+username.
	status, env, raw := getJSON(t, srv, "/api/auth/me", tok)
	if status != http.StatusOK {
		t.Fatalf("/me with valid token: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/me envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /me data: %v body=%s", err, raw)
	}
	if data.User.ID != uid {
		t.Fatalf("/me user.id = %q, want %q", data.User.ID, uid)
	}
	if data.User.Username != username {
		t.Fatalf("/me user.username = %q, want %q", data.User.Username, username)
	}

	// No Authorization header → 401.
	status, env, raw = getJSON(t, srv, "/api/auth/me", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("/me without bearer: status %d, want 401; body=%s", status, raw)
	}
	if env.OK || env.Error == nil || env.Error.Code == "" {
		t.Fatalf("/me without bearer: envelope ok=%v error=%v", env.OK, env.Error)
	}

	// Garbage bearer → 401.
	status, _, raw = getJSON(t, srv, "/api/auth/me", "not-a-jwt")
	if status != http.StatusUnauthorized {
		t.Fatalf("/me with garbage bearer: status %d, want 401; body=%s", status, raw)
	}

	// Tampered signature: splice another user's signature segment onto
	// alice's header+payload. Computed over a different payload, the
	// borrowed HMAC cannot validate the spliced token. Flipping a single
	// base64url char (the previous approach) only changes the trailing
	// 2 bits of the decoded signature and left ~20% of tokens unchanged
	// after re-decoding — see #447.
	_, otherTok := register(t, srv, "mallory", randomSecret(t, 12))
	aliceParts := strings.Split(tok, ".")
	otherParts := strings.Split(otherTok, ".")
	if len(aliceParts) != 3 || len(otherParts) != 3 {
		t.Fatalf("test JWTs not 3 segments: alice=%q other=%q", tok, otherTok)
	}
	if aliceParts[2] == otherParts[2] {
		t.Fatalf("two tokens share a signature segment, cannot tamper deterministically: %q", aliceParts[2])
	}
	tampered := aliceParts[0] + "." + aliceParts[1] + "." + otherParts[2]
	status, _, raw = getJSON(t, srv, "/api/auth/me", tampered)
	if status != http.StatusUnauthorized {
		t.Fatalf("/me with tampered signature: status %d, want 401; body=%s", status, raw)
	}
}
