package goclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goclient "hackathon/packages/go-client"
)

const fixtureChannelID = "01ABCDEFGHJKMNPQRSTVWXYZ00"

func TestListMessagesNoOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/channels/"+fixtureChannelID+"/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(envelopeJSON(`{"messages":[]}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	out, err := c.ListMessages(context.Background(), fixtureChannelID, goclient.ListMessagesOptions{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty, got %+v", out)
	}
}

func TestListMessagesWithCursorAndLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("before") != "01CURSORAAAAAAAAAAAAAAAAAA" {
			t.Errorf("before = %q", q.Get("before"))
		}
		if q.Get("limit") != "25" {
			t.Errorf("limit = %q", q.Get("limit"))
		}
		_, _ = w.Write([]byte(envelopeJSON(
			`{"messages":[{"id":"01MSGAAAAAAAAAAAAAAAAAAAAA","channel_id":"` +
				fixtureChannelID + `","sender_user_id":"u1","body":"hi","created_at":"2026-05-03T10:00:00Z"}]}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	out, err := c.ListMessages(context.Background(), fixtureChannelID, goclient.ListMessagesOptions{
		Before: "01CURSORAAAAAAAAAAAAAAAAAA",
		Limit:  25,
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(out) != 1 || out[0].Body != "hi" {
		t.Fatalf("messages = %+v", out)
	}
}

func TestPostMessage(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(envelopeJSON(
			`{"id":"01MSGAAAAAAAAAAAAAAAAAAAAA","channel_id":"` +
				fixtureChannelID + `","sender_user_id":"u1","body":"hello","created_at":"2026-05-03T10:00:00Z"}`,
		)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	msg, err := c.PostMessage(context.Background(), fixtureChannelID, goclient.PostMessageOptions{Body: "hello"})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("non-JSON body: %s", gotBody)
	}
	if sent["body"] != "hello" {
		t.Fatalf("body = %s", gotBody)
	}
	if msg.ID == "" || msg.Body != "hello" {
		t.Fatalf("msg = %+v", msg)
	}
}

func TestPostMessageNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(envelopeError("not_found", "channel not found")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok"))
	_, err := c.PostMessage(context.Background(), fixtureChannelID, goclient.PostMessageOptions{Body: "hi"})
	if !goclient.IsCode(err, "not_found") {
		t.Fatalf("err = %v, want not_found", err)
	}
}
