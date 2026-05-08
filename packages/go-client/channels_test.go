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

func TestListChannels(t *testing.T) {
	created := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/channels" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := json.Marshal(map[string]interface{}{
			"channels": []map[string]interface{}{
				{"id": "01ABCDEFGHJKMNPQRSTVWXYZ00", "name": "general", "created_at": created},
			},
		})
		_, _ = w.Write([]byte(`{"ok":true,"data":` + string(body) + `,"error":null}`))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	out, err := c.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(out) != 1 || out[0].Name != "general" {
		t.Fatalf("channels = %+v", out)
	}
	if !out[0].CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt mismatch: %v", out[0].CreatedAt)
	}
}

func TestCreateChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/channels" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		var got map[string]string
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("non-JSON body: %s", b)
		}
		if got["name"] != "random" {
			t.Errorf("name = %q", got["name"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(envelopeJSON(
			`{"id":"01ABCDEFGHJKMNPQRSTVWXYZ01","name":"random","created_at":"2026-05-03T10:00:00Z"}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	ch, err := c.CreateChannel(context.Background(), "random")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.Name != "random" || ch.ID == "" {
		t.Fatalf("channel = %+v", ch)
	}
}

func TestCreateChannelConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(envelopeError("conflict", "channel name already taken")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	_, err := c.CreateChannel(context.Background(), "general")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !goclient.IsCode(err, "conflict") {
		t.Fatalf("err = %v, want conflict code", err)
	}
}

func TestRenameChannel(t *testing.T) {
	const id = "01ABCDEFGHJKMNPQRSTVWXYZ02"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/channels/"+id {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		var got map[string]string
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("non-JSON body: %s", b)
		}
		if got["name"] != "renamed" {
			t.Errorf("name = %q", got["name"])
		}
		_, _ = w.Write([]byte(envelopeJSON(
			`{"id":"` + id + `","name":"renamed","created_at":"2026-05-03T10:00:00Z"}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	ch, err := c.RenameChannel(context.Background(), id, "renamed")
	if err != nil {
		t.Fatalf("RenameChannel: %v", err)
	}
	if ch.Name != "renamed" || string(ch.ID) != id {
		t.Fatalf("channel = %+v", ch)
	}
}

func TestRenameChannelErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
	}{
		{"BadRequest", http.StatusBadRequest, "invalid_request"},
		{"Forbidden", http.StatusForbidden, "forbidden"},
		{"NotFound", http.StatusNotFound, "not_found"},
		{"Conflict", http.StatusConflict, "conflict"},
		{"RateLimited", http.StatusTooManyRequests, "rate_limited"},
		{"Internal", http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(envelopeError(tc.code, "boom")))
			}))
			defer srv.Close()

			c := goclient.New(srv.URL, goclient.WithToken("tok"))
			_, err := c.RenameChannel(context.Background(), "01ABCDEFGHJKMNPQRSTVWXYZ02", "renamed")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !goclient.IsCode(err, tc.code) {
				t.Fatalf("err = %v, want code %q", err, tc.code)
			}
		})
	}
}
