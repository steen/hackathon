package testsupport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// Envelope mirrors the on-the-wire shape from
// apps/server/internal/http/errors.go (PRD §10). Tests use it via JSON
// so they stay decoupled from the production type.
type Envelope struct {
	OK    bool             `json:"ok"`
	Data  *json.RawMessage `json:"data"`
	Error *EnvelopeError   `json:"error"`
}

// EnvelopeError is the {code,message} pair under the error key.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PostJSON POSTs body to httpURL+path with optional bearer. Returns
// the status, parsed envelope, and the raw body bytes for callers
// that need byte-identical comparisons.
func PostJSON(t *testing.T, httpURL, path, bearer string, body any) (int, Envelope, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s body: %v", path, err)
		}
	}
	// noctx: client.Timeout below bounds the request; this is a
	// loopback-only test helper so context propagation has no caller
	// to surface to.
	req, err := http.NewRequest(http.MethodPost, httpURL+path, &buf) //nolint:noctx // see comment
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s %s: %v", req.Method, req.URL, err)
	}
	var env Envelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode envelope from %s %s (status %d): %v\nbody=%q", req.Method, req.URL, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode, env, raw
}

// Register POSTs /api/auth/register with the supplied invite code,
// fails the test on a non-2xx, and returns (userID, token) parsed
// from the success envelope.
func Register(t *testing.T, httpURL, inviteCode, username, password string) (userID, token string) {
	t.Helper()
	status, env, raw := PostJSON(t, httpURL, "/api/auth/register", "", map[string]string{
		"username":    username,
		"password":    password,
		"invite_code": inviteCode,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register %s: status %d body %s", username, status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("register %s: envelope ok=%v data=%v", username, env.OK, env.Data)
	}
	var data struct {
		Token string `json:"token"`
		User  struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v body=%s", err, raw)
	}
	if data.User.ID == "" {
		t.Fatalf("register %s: empty user id (body=%s)", username, raw)
	}
	return data.User.ID, data.Token
}

// MintTicket POSTs /api/auth/ws-ticket with the given bearer and
// returns the one-shot ticket string. Fails the test on non-200 or a
// missing ticket.
func MintTicket(t *testing.T, httpURL, bearer string) string {
	t.Helper()
	status, env, raw := PostJSON(t, httpURL, "/api/auth/ws-ticket", bearer, nil)
	if status != http.StatusOK {
		t.Fatalf("/ws-ticket: status %d body %s", status, raw)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("/ws-ticket envelope ok=%v data=%v", env.OK, env.Data)
	}
	var data struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(*env.Data, &data); err != nil {
		t.Fatalf("decode /ws-ticket data: %v body=%s", err, raw)
	}
	if data.Ticket == "" {
		t.Fatalf("/ws-ticket: empty ticket (body=%s)", raw)
	}
	return data.Ticket
}
