package auth_endpoints_e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// AC-1: POST /api/auth/register requires a valid invite code
// (CHAT_INVITE_CODE) and creates a user with a hashed password (US-1, US-11).
func TestAC1_Register_RequiresInviteCode_PersistsHashedPassword(t *testing.T) {
	srv := startServer(t)

	const username = "alice"
	password := randomSecret(t, 12) // 24 hex chars > 10 char minimum

	// Wrong invite code → forbidden envelope.
	status, env, raw := postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": "wrong-" + randomSecret(t, 4),
	})
	if status < 400 || status >= 500 {
		t.Fatalf("wrong invite_code: status %d, want 4xx; body=%s", status, raw)
	}
	if env.OK || env.Error == nil {
		t.Fatalf("wrong invite_code: envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}

	// Missing invite_code field → 4xx envelope. DisallowUnknownFields means
	// we send a body with no invite_code at all.
	status, env, raw = postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username": username,
		"password": password,
	})
	if status < 400 || status >= 500 {
		t.Fatalf("missing invite_code: status %d, want 4xx; body=%s", status, raw)
	}
	if env.OK || env.Error == nil {
		t.Fatalf("missing invite_code: envelope ok=%v error=%v body=%s", env.OK, env.Error, raw)
	}

	// Right invite_code → 200/201 envelope with ULID id and the username.
	status, env, raw = postJSON(t, srv, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": srv.inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register: status %d, want 200/201; body=%s", status, raw)
	}
	if !env.OK || env.Error != nil || env.Data == nil {
		t.Fatalf("register: envelope ok=%v error=%v data=%v", env.OK, env.Error, env.Data)
	}

	// Pull user.id out of the envelope and assert ULID shape.
	var data struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.User.Username != username {
		t.Fatalf("user.username = %q, want %q", data.User.Username, username)
	}
	if len(data.User.ID) != 26 {
		t.Fatalf("user.id %q has length %d, want 26 (ULID)", data.User.ID, len(data.User.ID))
	}

	// Reach into the on-disk SQLite to assert the password is hashed.
	db := openDBReadOnly(t, srv)
	var dbUsername, dbHash string
	if err := db.QueryRow(
		`SELECT username, password_hash FROM users WHERE username = ?`, username,
	).Scan(&dbUsername, &dbHash); err != nil {
		t.Fatalf("select user: %v", err)
	}
	if dbUsername != username {
		t.Fatalf("DB username %q != %q", dbUsername, username)
	}
	if dbHash == password {
		t.Fatalf("password stored in plaintext")
	}
	if !strings.HasPrefix(dbHash, "$2a$") && !strings.HasPrefix(dbHash, "$2b$") && !strings.HasPrefix(dbHash, "$2y$") {
		t.Fatalf("password_hash %q lacks bcrypt prefix", dbHash)
	}
}
