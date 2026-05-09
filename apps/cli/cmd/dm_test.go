package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	goclient "hackathon/packages/go-client"
)

// dmWSRig spins up the minimum HTTP+WS surface goclient.Watch needs
// (POST /api/auth/ws-ticket, GET /ws). Each frame in frames is written
// in order, then the connection is closed so dmStreamOnce returns.
type dmWSRig struct {
	server *httptest.Server
	frames [][]byte
}

func newDMWSRig(t *testing.T, frames [][]byte) *dmWSRig {
	t.Helper()
	rig := &dmWSRig{frames: frames}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/ws-ticket", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"ticket":     "tkt",
				"expires_at": time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339Nano),
			},
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		for _, f := range rig.frames {
			if err := c.Write(ctx, websocket.MessageText, f); err != nil {
				return
			}
		}
	})
	rig.server = httptest.NewServer(mux)
	t.Cleanup(rig.server.Close)
	return rig
}

func TestDMStreamOnceWarnsOnUnparseableFrame(t *testing.T) {
	// First frame: malformed dm (data is a string, not the expected
	// object with conversation+dm_message). Second frame: malformed
	// again so we can prove the throttle suppresses it.
	bad := []byte(`{"type":"dm","data":"not-an-object"}`)
	rig := newDMWSRig(t, [][]byte{bad, bad})

	client := goclient.New(rig.server.URL, goclient.WithToken("tok"))
	stdout := &safeBuf{}
	stderr := &safeBuf{}
	env := &Env{Stdout: stdout, Stderr: stderr}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := dmStreamOnce(ctx, env, client, ""); err != nil {
		t.Fatalf("dmStreamOnce: %v", err)
	}

	got := stderr.String()
	if !strings.Contains(got, "dm watch: drop unparseable frame:") {
		t.Fatalf("stderr missing warning; got %q", got)
	}
	// Throttle is 5s; both bad frames happen within microseconds, so
	// only one warning should appear.
	if c := strings.Count(got, "dm watch: drop unparseable frame:"); c != 1 {
		t.Fatalf("warning count = %d, want 1 (throttled); stderr=%q", c, got)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestDMStreamOncePassesValidFrames(t *testing.T) {
	// Build a minimally valid dm frame. The handlers under test only
	// read DMMessage fields, so we can leave most of Conversation empty.
	good := []byte(`{
		"type":"dm",
		"data":{
			"conversation":{"id":"01HZZZZZZZZZZZZZZZZZZZZZZZ","peer":{"id":"01HAAAAAAAAAAAAAAAAAAAAAAA","username":"bob"},"unread_count":0,"last_message_at":null},
			"dm_message":{"id":"01HMMMMMMMMMMMMMMMMMMMMMMM","conversation_id":"01HZZZZZZZZZZZZZZZZZZZZZZZ","sender_user_id":"01HAAAAAAAAAAAAAAAAAAAAAAA","body":"hi","created_at":"2025-01-01T00:00:00Z"}
		}
	}`)
	rig := newDMWSRig(t, [][]byte{good})

	client := goclient.New(rig.server.URL, goclient.WithToken("tok"))
	stdout := &safeBuf{}
	stderr := &safeBuf{}
	env := &Env{Stdout: stdout, Stderr: stderr}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := dmStreamOnce(ctx, env, client, ""); err != nil {
		t.Fatalf("dmStreamOnce: %v", err)
	}

	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty (frame is valid)", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\thi\n") {
		t.Fatalf("stdout missing message body; got %q", stdout.String())
	}
}
