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
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/wsproto"
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

// MessageBodyLimit and MessageBodyLimitCloseReason re-export the
// wsproto canonical values so existing wsapi callers (handler logic,
// internal tests) keep their import path unchanged. Both definitions
// live in wsproto so the close-reason text is derived from the byte
// count at one source of truth — the e2e tests under tests/e2e/ can
// reference them without tripping Go's internal-package rule.
//
// PRD §9, SEC-8. Mirrors internal/http.MessageBodyLimit; the WS path
// enforces it independently so wsapi has no HTTP-side dependency.
const MessageBodyLimit = wsproto.MessageBodyLimit

// MessageBodyLimitCloseReason aliases the wsproto var. It cannot be a
// const because the wire text is computed via fmt.Sprintf to stay in
// sync with MessageBodyLimit.
var MessageBodyLimitCloseReason = wsproto.MessageBodyLimitCloseReason

// Config carries the per-handler dependencies that vary between
// production wiring and tests. OriginPatterns is forwarded to
// coder/websocket.AcceptOptions; same-origin (Host == Origin) is
// always allowed by the library and does not need to be listed.
//
// ChannelLookup, when non-nil, is invoked before websocket.Accept for
// any requested channel that is not the legacy defaultChannel sentinel
// (#general). On (false, nil) the upgrade is rejected with HTTP 404
// "channel not found" before the WebSocket handshake. On a non-nil
// error the upgrade is rejected with HTTP 500 and the error is logged.
type Config struct {
	OriginPatterns []string
	ChannelLookup  func(ctx context.Context, id string) (bool, error)
}

// connSubscriber bridges hub.Subscriber to a websocket.Conn via a buffered
// queue so a slow client cannot stall the hub. When the queue is full the
// message is dropped for that subscriber.
//
// userID and channel are bound at connect time so messages.user_id writes
// can attribute the sender (gap-D wiring). Both are read-only after
// construction; no mutex needed.
type connSubscriber struct {
	send    chan []byte
	done    chan struct{}
	userID  string
	channel string
}

func newConnSubscriber(userID, channel string) *connSubscriber {
	return &connSubscriber{
		send:    make(chan []byte, sendBuffer),
		done:    make(chan struct{}),
		userID:  userID,
		channel: channel,
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
// use 1008). The channel-existence check (when cfg.ChannelLookup is set)
// runs before redemption so an unknown channel rejects with 404 without
// consuming the one-shot ticket (audit #78, low-severity).
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
			// Upper-fold non-sentinel channel ids so a lower-cased
			// URL hits the same channel as the REST surface, which
			// also folds via ids.NormalizeChannelID (audit #78, info).
			// The defaultChannel sentinel ("#general") is a literal
			// and must NOT be folded.
			if c != defaultChannel {
				norm, ok := ids.NormalizeChannelID(c)
				if !ok {
					http.Error(w, "channel not found", http.StatusNotFound)
					return
				}
				channel = norm
			} else {
				channel = c
			}
		}

		// Reject upgrades for unknown channels with HTTP 404 BEFORE the
		// WebSocket handshake — and BEFORE redeeming the ws-ticket, so a
		// typo or probe does not burn the one-shot ticket (audit #78,
		// low-severity). The legacy defaultChannel sentinel skips the
		// lookup so phase-0 boot paths and tests without a DB keep
		// working. Use http.Error (text/plain) — the JSON envelope lives
		// in internal/http and importing it here would create a cycle
		// (the http package's ws_broadcast_test.go imports wsapi).
		if cfg.ChannelLookup != nil && channel != defaultChannel {
			ok, err := cfg.ChannelLookup(r.Context(), channel)
			if err != nil {
				log.Printf("ws channel lookup: %v", err)
				http.Error(w, "channel lookup failed", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
		}

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

		conn, err := websocket.Accept(w, r, acceptOpts)
		if err != nil {
			return
		}
		// CloseNow's error is non-actionable in defer: by the time it returns,
		// the underlying TCP connection is gone either way.
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(ReadLimitBytes)

		sub := newConnSubscriber(userID, channel)
		h.Subscribe(channel, sub)
		defer h.Unsubscribe(channel, sub)
		defer sub.close()

		// Presence events fire only for authenticated connections.
		// Order matters: Subscribe before AddPresence so the joining
		// user's own connection is in the broadcast target set —
		// otherwise the first connection for a user would never see
		// its own join event.
		if userID != "" {
			if first := h.AddPresence(userID); first {
				h.BroadcastAll(presenceFrame(presenceJoin, userID))
			}
			defer func() {
				if last := h.RemovePresence(userID); last {
					h.BroadcastAll(presenceFrame(presenceLeave, userID))
				}
			}()
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go writeLoop(ctx, conn, sub)
		readLoop(ctx, conn, newTokenBucket(SendRateBurst, SendRatePerSec))
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, bucket *tokenBucket) {
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
			_ = conn.Close(websocket.StatusMessageTooBig, MessageBodyLimitCloseReason)
			return
		}
		if !bucket.allow() {
			_ = conn.Close(websocket.StatusPolicyViolation, "send rate limit exceeded")
			return
		}
		// Audit #78 (medium): drop inbound frames silently. The phase-0
		// raw rebroadcast let any peer forge {type,data} envelopes with
		// arbitrary sender_user_id, bypassing persistence and audit log.
		// Producers must use POST /api/channels/{id}/messages so the
		// server attributes the sender from the JWT and persists first.
		// The read still happens (drains the buffer; enforces size +
		// rate limits above) — only the broadcast is gone.
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
