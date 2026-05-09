package goclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goclient "hackathon/packages/go-client"
)

func TestListUsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/users" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		_, _ = w.Write([]byte(envelopeJSON(
			`{"users":[{"id":"u1","username":"alice"},{"id":"u2","username":"bob"}]}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	users, err := c.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].ID != "u1" || users[0].Username != "alice" {
		t.Errorf("users[0] = %+v", users[0])
	}
	if users[1].ID != "u2" || users[1].Username != "bob" {
		t.Errorf("users[1] = %+v", users[1])
	}
}

func TestListUsersEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(`{"users":[]}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	users, err := c.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("len(users) = %d, want 0", len(users))
	}
}

func TestListUsersUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(envelopeError("unauthorized", "missing token")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	_, err := c.ListUsers(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !goclient.IsCode(err, "unauthorized") {
		t.Fatalf("err = %v, want unauthorized code", err)
	}
}
