package goclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

func TestLoginStoresToken(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(envelopeJSON(`{"token":"jwt-abc","user":{"id":"u1","username":"alice"}}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	resp, err := c.Login(context.Background(), "alice", "hunter2-pw-aa")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if gotPath != "/api/auth/login" || gotMethod != http.MethodPost {
		t.Fatalf("path/method = %q %q", gotPath, gotMethod)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("server saw non-JSON body: %s", gotBody)
	}
	if sent["username"] != "alice" || sent["password"] != "hunter2-pw-aa" {
		t.Fatalf("body = %s", gotBody)
	}
	if resp.Token != "jwt-abc" || resp.User.Username != "alice" {
		t.Fatalf("decoded resp = %+v", resp)
	}
	if c.Token() != "jwt-abc" {
		t.Fatalf("Login should have called SetToken, got %q", c.Token())
	}
}

func TestRegisterStoresToken(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(envelopeJSON(`{"token":"jwt-new","user":{"id":"u2","username":"bob"}}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	resp, err := c.Register(context.Background(), "bob", "hunter2-pw-bb", "test-invite-aaaaaaaa")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("non-JSON body: %s", gotBody)
	}
	if sent["invite_code"] != "test-invite-aaaaaaaa" {
		t.Fatalf("invite_code missing: %s", gotBody)
	}
	if resp.Token != "jwt-new" {
		t.Fatalf("token = %q", resp.Token)
	}
	if c.Token() != "jwt-new" {
		t.Fatalf("Register should set token")
	}
}

func TestMeReturnsUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/me" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(envelopeJSON(`{"user":{"id":"u1","username":"alice"}}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	u, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if u.ID != "u1" || u.Username != "alice" {
		t.Fatalf("user = %+v", u)
	}
}

func TestLogoutClearsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(`{"ok":true}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok-pre"))
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if c.Token() != "" {
		t.Fatalf("Logout should have cleared token, got %q", c.Token())
	}
}

func TestLogoutKeepsTokenOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(envelopeError("internal", "boom")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok-pre"))
	if err := c.Logout(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
	if c.Token() != "tok-pre" {
		t.Fatalf("token should survive a failed logout, got %q", c.Token())
	}
}

func TestWsTicketDecodesExpiry(t *testing.T) {
	want := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(
			`{"ticket":"deadbeef","expires_at":"` + want.Format(time.RFC3339Nano) + `"}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	tk, err := c.WsTicket(context.Background())
	if err != nil {
		t.Fatalf("WsTicket: %v", err)
	}
	if tk.Ticket != "deadbeef" {
		t.Fatalf("Ticket = %q", tk.Ticket)
	}
	if !tk.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", tk.ExpiresAt, want)
	}
}

func TestLoginSurfacesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(envelopeError("unauthorized", "invalid username or password")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	_, err := c.Login(context.Background(), "alice", "wrong-pw-aaaaa")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !goclient.IsCode(err, "unauthorized") {
		t.Fatalf("error = %v, want unauthorized code", err)
	}
	if c.Token() != "" {
		t.Fatalf("failed login should not set token")
	}
}
