// Package wsapi exposes the /ws HTTP handler that bridges WebSocket
// connections to the in-memory hub.
package wsapi

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
)

const (
	defaultChannel = "#general"
	sendBuffer     = 64
)

// ReadLimitBytes caps a single inbound WS frame (PRD §9, SEC-6).
// Hitting this causes the library to close with StatusMessageTooBig (1009).
const ReadLimitBytes int64 = 64 * 1024

// SendRateBurst / SendRatePerSec implement the per-connection send rate
// limit from PRD §9: 10 msg/s, burst 30. Excess inbound frames drop the
// connection with StatusPolicyViolation (1008).
const (
	SendRateBurst  = 30
	SendRatePerSec = 10.0
)

// MessageBodyLimit caps the decoded chat-message body (PRD §9, SEC-8).
// Mirrors internal/http.MessageBodyLimit; the WS path enforces it
// independently so wsapi has no HTTP-side dependency.
const MessageBodyLimit = 4 * 1024

// Config carries the per-handler dependencies that vary between
// production wiring and tests. OriginPatterns is forwarded to
// coder/websocket.AcceptOptions; same-origin (Host == Origin) is
// always allowed by the library and does not need to be listed.
type Config struct {
	OriginPatterns []string
}

// connSubscriber bridges hub.Subscriber to a websocket.Conn via a buffered
// queue so a slow client cannot stall the hub. When the queue is full the
// message is dropped for that subscriber.
type connSubscriber struct {
	send chan []byte
	done chan struct{}
}

func newConnSubscriber() *connSubscriber {
	return &connSubscriber{
		send: make(chan []byte, sendBuffer),
		done: make(chan struct{}),
	}
}

// Send queues msg for delivery to this subscriber. Drops on overflow rather
// than blocking the hub.
func (c *connSubscriber) Send(msg []byte) {
	select {
	case c.send <- msg:
	case <-c.done:
	default:
		// Drop on overflow rather than block the hub.
	}
}

func (c *connSubscriber) close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}

// Handler returns an http.HandlerFunc serving the /ws endpoint.
//
// When ts is non-nil, the handler enforces SEC-12: every upgrade must
// present a ?ticket=<hex> query parameter that TicketStore.Redeem
// accepts. The redemption happens before the WebSocket handshake so a
// rejection is a 401 with the PRD §10 envelope (RFC 6455 only defines
// close codes after a successful upgrade — pre-upgrade failures cannot
// use 1008).
//
// When ts is nil, ticket enforcement is skipped. This branch exists
// for the phase-0 smoke wiring and for tests that exercise the hub
// fan-out without standing up the auth stack.
//
// Same-origin enforcement is delegated to coder/websocket.Accept,
// which compares Host to Origin by default and additionally honors
// any patterns in cfg.OriginPatterns. A mismatch yields HTTP 403.
//
// Each connection subscribes to one channel for its lifetime:
// defaultChannel by default, or the value of the ?channel= query
// parameter when present (channels-and-messages feature). The PRD's
// typed {type:subscribe,...} frame protocol can layer on top later
// without breaking this contract.
func Handler(h *hub.Hub, ts *auth.TicketStore, cfg Config) http.HandlerFunc {
	acceptOpts := &websocket.AcceptOptions{
		OriginPatterns: cfg.OriginPatterns,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var userID string
		if ts != nil {
			ticket := r.URL.Query().Get("ticket")
			if ticket == "" {
				// Same body + code for missing-vs-invalid so a probing
				// client cannot distinguish the two arms (SEC-12).
				http.Error(w, "invalid ws ticket", http.StatusUnauthorized)
				return
			}
			uid, ok := ts.Redeem(ticket)
			if !ok {
				http.Error(w, "invalid ws ticket", http.StatusUnauthorized)
				return
			}
			userID = uid
		}

		channel := defaultChannel
		if c := r.URL.Query().Get("channel"); c != "" {
			// Cap the channel-key length — a 1 MB query string would
			// otherwise sit in the subscriber map for the lifetime of
			// the connection. 64 chars covers a 26-char ULID plus
			// padding for `#general`-style legacy names.
			if len(c) > 64 {
				http.Error(w, "channel parameter too long", http.StatusBadRequest)
				return
			}
			channel = c
		}

		conn, err := websocket.Accept(w, r, acceptOpts)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(ReadLimitBytes)

		// TODO(channels-and-messages): bind userID onto a per-connection
		// state struct so messages.user_id writes can attribute the
		// sender. The redemption above already satisfies SEC-12; this
		// TODO is the seam for the next feature, not a security gap.
		_ = userID

		sub := newConnSubscriber()
		h.Subscribe(channel, sub)
		defer h.Unsubscribe(channel, sub)
		defer sub.close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go writeLoop(ctx, conn, sub)
		readLoop(ctx, conn, h, channel, newTokenBucket(SendRateBurst, SendRatePerSec))
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, h *hub.Hub, channel string, bucket *tokenBucket) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) || ctx.Err() != nil {
				return
			}
			log.Printf("ws read: %v", err)
			return
		}
		if len(data) > MessageBodyLimit {
			_ = conn.Close(websocket.StatusMessageTooBig, "message body exceeds 4 KiB limit")
			return
		}
		if !bucket.allow() {
			_ = conn.Close(websocket.StatusPolicyViolation, "send rate limit exceeded")
			return
		}
		// Phase-0 AC-3 contract: inbound WS frames are rebroadcast to
		// every subscriber of the same channel. Phase-1 added a parallel
		// REST producer (`POST /api/channels/{id}/messages`) which emits
		// a `{"type":"message","data":<Message>}` envelope; the two
		// shapes coexist on the wire today. A future feature will
		// converge them by parsing inbound frames through the same
		// envelope, but removing this raw rebroadcast now would regress
		// the phase-0 AC.
		h.Broadcast(channel, data)
	}
}

func writeLoop(ctx context.Context, conn *websocket.Conn, sub *connSubscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.done:
			return
		case msg := <-sub.send:
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
	}
}
