package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
)

// TestPresenceJoinFiresOnFirstConnection asserts that the WS handler
// emits {type:"presence", data:{kind:"join", user_id}} once the first
// authenticated connection for a user lands.
func TestPresenceJoinFiresOnFirstConnection(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	// Observer connects first (#general, no auth needed) so we can see
	// the join event for the next user. Using nil ticket store is not
	// possible here because we want to test the auth path; instead
	// observer uses its own ticket so we can isolate user IDs.
	observerTok, _ := ts.Issue("observer")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	observer, _, err := websocket.Dial(ctx, wsURL+"?ticket="+observerTok, nil)
	if err != nil {
		t.Fatalf("observer dial: %v", err)
	}
	defer observer.CloseNow()

	if err := waitForSubscribers(h, "#general", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	// Drain the observer's own join (server broadcasts the first
	// presence event to all subscribers, including the joining user).
	if ev, err := readPresence(ctx, observer); err != nil {
		t.Fatalf("observer self-join: %v", err)
	} else if ev.Kind != "join" || ev.UserID != "observer" {
		t.Fatalf("observer self-join: got kind=%s id=%s", ev.Kind, ev.UserID)
	}

	// New user joins — observer should see the presence frame.
	aliceTok, _ := ts.Issue("alice")
	alice, _, err := websocket.Dial(ctx, wsURL+"?ticket="+aliceTok, nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	defer alice.CloseNow()

	ev, err := readPresence(ctx, observer)
	if err != nil {
		t.Fatalf("observer read alice's join: %v", err)
	}
	if ev.Kind != "join" || ev.UserID != "alice" {
		t.Fatalf("got kind=%s id=%s, want join/alice", ev.Kind, ev.UserID)
	}
}

// TestPresenceLeaveFiresOnLastDisconnect asserts the leave event lands
// when the last connection for a user closes.
func TestPresenceLeaveFiresOnLastDisconnect(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	observerTok, _ := ts.Issue("observer")
	observer, _, err := websocket.Dial(ctx, wsURL+"?ticket="+observerTok, nil)
	if err != nil {
		t.Fatalf("observer dial: %v", err)
	}
	defer observer.CloseNow()
	if _, err := readPresence(ctx, observer); err != nil {
		t.Fatalf("observer self-join: %v", err)
	}

	aliceTok, _ := ts.Issue("alice")
	alice, _, err := websocket.Dial(ctx, wsURL+"?ticket="+aliceTok, nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	if ev, err := readPresence(ctx, observer); err != nil || ev.Kind != "join" {
		t.Fatalf("alice join: ev=%+v err=%v", ev, err)
	}

	_ = alice.Close(websocket.StatusNormalClosure, "")

	ev, err := readPresence(ctx, observer)
	if err != nil {
		t.Fatalf("observer read alice's leave: %v", err)
	}
	if ev.Kind != "leave" || ev.UserID != "alice" {
		t.Fatalf("got kind=%s id=%s, want leave/alice", ev.Kind, ev.UserID)
	}
}

// TestPresenceMultipleConnectionsCountAsOneUser asserts that a user
// who opens two connections only triggers one join, and only triggers
// a leave after the second connection closes. We cannot use a
// timeout-bound Read to "prove no event arrived" — coder/websocket
// tears down the conn when its read context is cancelled. Instead we
// run a background drainer that collects every inbound frame into a
// channel, then assert on what arrived (or didn't) at sync points.
func TestPresenceMultipleConnectionsCountAsOneUser(t *testing.T) {
	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	observerTok, _ := ts.Issue("observer")
	observer, _, err := websocket.Dial(ctx, wsURL+"?ticket="+observerTok, nil)
	if err != nil {
		t.Fatalf("observer dial: %v", err)
	}
	defer observer.CloseNow()

	frames := make(chan presenceEvent, 16)
	go func() {
		for {
			_, data, err := observer.Read(ctx)
			if err != nil {
				return
			}
			var ev presenceEvent
			if err := json.Unmarshal(data, &ev); err == nil {
				frames <- ev
			}
		}
	}()

	// Self-join.
	expectPresence(t, frames, "join", "observer")

	// alice connects twice.
	aliceTok1, _ := ts.Issue("alice")
	aliceTok2, _ := ts.Issue("alice")
	alice1, _, err := websocket.Dial(ctx, wsURL+"?ticket="+aliceTok1, nil)
	if err != nil {
		t.Fatalf("alice1 dial: %v", err)
	}
	defer alice1.CloseNow()
	expectPresence(t, frames, "join", "alice")

	alice2, _, err := websocket.Dial(ctx, wsURL+"?ticket="+aliceTok2, nil)
	if err != nil {
		t.Fatalf("alice2 dial: %v", err)
	}
	if h.PresenceCount() != 2 { // observer + alice
		t.Fatalf("PresenceCount = %d, want 2", h.PresenceCount())
	}

	// Close one of alice's connections. No leave event should fire
	// because alice still has a connection open.
	_ = alice2.Close(websocket.StatusNormalClosure, "")
	if err := waitForPresenceCount(h, 2, 1*time.Second); err != nil {
		t.Fatal(err)
	}
	expectNoFrame(t, frames, 200*time.Millisecond)

	// Now close the last alice connection — leave should fire.
	_ = alice1.Close(websocket.StatusNormalClosure, "")
	expectPresence(t, frames, "leave", "alice")
}

// expectPresence waits for the next presence frame and asserts on its
// kind + user_id. Fails the test on timeout or mismatch.
func expectPresence(t *testing.T, frames <-chan presenceEvent, wantKind, wantUserID string) {
	t.Helper()
	select {
	case ev := <-frames:
		if ev.Type != PresenceEvent || ev.Data.Kind != wantKind || ev.Data.UserID != wantUserID {
			t.Fatalf("got %+v, want kind=%s user_id=%s", ev, wantKind, wantUserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for presence kind=%s user_id=%s", wantKind, wantUserID)
	}
}

// expectNoFrame asserts that no frame arrives within d. Used to prove
// the negative case (e.g. partial disconnect must not emit a leave).
func expectNoFrame(t *testing.T, frames <-chan presenceEvent, d time.Duration) {
	t.Helper()
	select {
	case ev := <-frames:
		t.Fatalf("expected no frame within %v, got %+v", d, ev)
	case <-time.After(d):
	}
}

func waitForPresenceCount(h *hub.Hub, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if got := h.PresenceCount(); got == want {
			return nil
		}
		if time.Now().After(deadline) {
			return &decodeErr{got: "presence count never reached target"}
		}
		// Poll: presence updates fire asynchronously off the hub join
		// path, and there is no synchronous "presence applied" signal —
		// keep the wait short so the deadline branch still wins on hangs.
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPresenceDoesNotFireWithoutAuth asserts that the unauthenticated
// (ticket store == nil) path produces no presence events — there is
// no userID to attribute the event to.
func TestPresenceDoesNotFireWithoutAuth(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(Handler(h, nil, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	// Confirm subscribed.
	if err := waitForSubscribers(h, "#general", 1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if h.PresenceCount() != 0 {
		t.Fatalf("unauth connection should not register presence; got %d", h.PresenceCount())
	}
}

type presenceEvent struct {
	Type string `json:"type"`
	Data struct {
		Kind     string `json:"kind"`
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	} `json:"data"`
}

// readPresence reads a single inbound frame and asserts it is a
// presence frame, returning the parsed kind/user_id.
func readPresence(ctx context.Context, c *websocket.Conn) (struct{ Kind, UserID string }, error) {
	var zero struct{ Kind, UserID string }
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := c.Read(readCtx)
	if err != nil {
		return zero, err
	}
	var ev presenceEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return zero, err
	}
	if ev.Type != PresenceEvent {
		return zero, &decodeErr{got: ev.Type}
	}
	return struct{ Kind, UserID string }{ev.Data.Kind, ev.Data.UserID}, nil
}

type decodeErr struct{ got string }

func (e *decodeErr) Error() string {
	return "expected presence frame, got type=" + e.got
}

// TestPresenceFrameCarriesUsernameWhenLookupRegistered asserts that join
// and leave frames embed `username` when the package-level resolver hook
// is set. The hook is unset on test exit so it does not leak into other
// tests in this package (#490).
func TestPresenceFrameCarriesUsernameWhenLookupRegistered(t *testing.T) {
	directory := map[string]string{
		"observer": "Olivia Observer",
		"alice":    "Alice Example",
	}
	SetPresenceUsernameLookup(func(userID string) string {
		return directory[userID]
	})
	t.Cleanup(func() { SetPresenceUsernameLookup(nil) })

	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	observerTok, _ := ts.Issue("observer")
	observer, _, err := websocket.Dial(ctx, wsURL+"?ticket="+observerTok, nil)
	if err != nil {
		t.Fatalf("observer dial: %v", err)
	}
	defer observer.CloseNow()

	// Self-join carries the observer's username.
	selfJoin, err := readPresenceEvent(ctx, observer)
	if err != nil {
		t.Fatalf("observer self-join: %v", err)
	}
	if selfJoin.Data.Kind != "join" || selfJoin.Data.UserID != "observer" {
		t.Fatalf("observer self-join: got kind=%s id=%s", selfJoin.Data.Kind, selfJoin.Data.UserID)
	}
	if selfJoin.Data.Username != "Olivia Observer" {
		t.Fatalf("observer self-join username: got %q want %q", selfJoin.Data.Username, "Olivia Observer")
	}

	// Alice joins → frame carries alice's username.
	aliceTok, _ := ts.Issue("alice")
	alice, _, err := websocket.Dial(ctx, wsURL+"?ticket="+aliceTok, nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	defer alice.CloseNow()

	join, err := readPresenceEvent(ctx, observer)
	if err != nil {
		t.Fatalf("observer read alice's join: %v", err)
	}
	if join.Data.Kind != "join" || join.Data.UserID != "alice" {
		t.Fatalf("alice join: got kind=%s id=%s", join.Data.Kind, join.Data.UserID)
	}
	if join.Data.Username != "Alice Example" {
		t.Fatalf("alice join username: got %q want %q", join.Data.Username, "Alice Example")
	}

	// Alice leaves → leave frame also carries the username.
	_ = alice.Close(websocket.StatusNormalClosure, "")

	leave, err := readPresenceEvent(ctx, observer)
	if err != nil {
		t.Fatalf("observer read alice's leave: %v", err)
	}
	if leave.Data.Kind != "leave" || leave.Data.UserID != "alice" {
		t.Fatalf("alice leave: got kind=%s id=%s", leave.Data.Kind, leave.Data.UserID)
	}
	if leave.Data.Username != "Alice Example" {
		t.Fatalf("alice leave username: got %q want %q", leave.Data.Username, "Alice Example")
	}
}

// TestPresenceFrameOmitsUsernameWhenLookupUnset asserts that without a
// resolver hook the frame's JSON has no `username` key — the wire shape
// stays byte-compatible with the pre-#490 contract so a partial rollout
// does not break old decoders.
func TestPresenceFrameOmitsUsernameWhenLookupUnset(t *testing.T) {
	SetPresenceUsernameLookup(nil)

	h := hub.New()
	ts := auth.NewTicketStore()
	srv := httptest.NewServer(Handler(h, ts, Config{}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tok, _ := ts.Issue("solo")
	c, _, err := websocket.Dial(ctx, wsURL+"?ticket="+tok, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	readCtx, cancelRead := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRead()
	_, raw, err := c.Read(readCtx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), `"username"`) {
		t.Fatalf("frame contains username key with no lookup registered: %s", string(raw))
	}
}

// readPresenceEvent reads a single frame and decodes it as a presence
// envelope (type + kind + user_id + optional username). Used by tests
// that need to assert on the username field, which the older
// readPresence helper hides behind a narrow return tuple.
func readPresenceEvent(ctx context.Context, c *websocket.Conn) (presenceEvent, error) {
	var ev presenceEvent
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := c.Read(readCtx)
	if err != nil {
		return ev, err
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return ev, err
	}
	if ev.Type != PresenceEvent {
		return ev, &decodeErr{got: ev.Type}
	}
	return ev, nil
}
