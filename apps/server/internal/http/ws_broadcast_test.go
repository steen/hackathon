package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/wsapi"
)

// US-5 — a WS subscriber on a channel receives the message frame
// emitted by POST /api/channels/{id}/messages within a short window.
// This is the end-to-end glue test: real WS upgrade, real hub, real
// HTTP handler.
func TestWSSubscriberReceivesBroadcastMessage(t *testing.T) {
	cf := newChannelsFixture(t)
	defer cf.close()
	tok := registerOK(t, cf.fixture, "alice", "correct-horse-battery")
	chID := createChannelOK(t, cf, tok, "general")

	// Stand up a real http.Server: the channels mux for /api/* and the
	// wsapi handler for /ws, sharing the same hub.
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/", cf.mux)
	mux.HandleFunc("/ws", wsapi.Handler(cf.hub, nil, wsapi.Config{}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?channel=" + chID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Wait for the hub to register the subscriber so the broadcast is
	// not lost to a race against the upgrade handshake.
	deadline := time.Now().Add(2 * time.Second)
	for cf.hub.SubscriberCount(chID) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("hub did not register subscriber within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	postURL := srv.URL + "/api/channels/" + chID + "/messages"
	envBody, err := json.Marshal(map[string]any{"envelope": fakeEnvelopeWire(time.Now())})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, postURL,
		strings.NewReader(string(envBody)))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusCreated {
		t.Fatalf("post status: %d", resp.StatusCode)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var frame struct {
		Type string       `json:"type"`
		Data repo.Message `json:"data"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(data))
	}
	if frame.Type != WSEventMessage || frame.Data.Envelope.CipherSuite != 0x01 {
		t.Fatalf("frame: %+v", frame)
	}
	if string(frame.Data.Envelope.Ciphertext) != "ciphertext" {
		t.Fatalf("ciphertext round-trip mismatch: %q", string(frame.Data.Envelope.Ciphertext))
	}
}
