// Package wsapi exposes the /ws HTTP handler that bridges WebSocket
// connections to the in-memory hub.
package wsapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/wsproto"
)

// testDefaultChannel is the channel a /ws upgrade lands on when the
// caller omits ?channel= AND the handler has no cfg.ChannelLookup
// wired. Production wiring always supplies a ChannelLookup (the SQLite
// repo's ChannelExists), so production rejects an upgrade missing
// ?channel= with HTTP 400 — this fallback exists only for the unit
// tests in apps/server/internal/wsapi/* that exercise hub fan-out
// without standing up a DB.
const testDefaultChannel = "#test-default"

const sendBuffer = 64

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

// MessageBodyLimit re-exports the wsproto constant so existing wsapi
// callers (handler logic, internal tests) keep their import path
// unchanged. The canonical definition lives in wsproto so the e2e
// tests under tests/e2e/ can reference it without tripping Go's
// internal-package rule.
//
// PRD §9, SEC-8. Mirrors internal/http.MessageBodyLimit; the WS path
// enforces it independently so wsapi has no HTTP-side dependency.
//
// The close-reason text (wsproto.MessageBodyLimitCloseReason) is a
// package-level var, so it is referenced at the call site rather than
// re-aliased here — an init-time copy would drift silently if the
// source were ever rebuilt at runtime.
const MessageBodyLimit = wsproto.MessageBodyLimit

// Config carries the per-handler dependencies that vary between
// production wiring and tests. OriginPatterns is forwarded to
// coder/websocket.AcceptOptions; same-origin (Host == Origin) is
// always allowed by the library and does not need to be listed.
//
// ChannelLookup, when non-nil, is invoked before websocket.Accept for
// the requested channel id. On (false, nil) the upgrade is rejected
// with HTTP 404 "channel not found" before the WebSocket handshake.
// On a non-nil error the upgrade is rejected with HTTP 500 and the
// error is logged. Production wiring always supplies this; nil is a
// test-only path.
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
//
// shutdown signals the writeLoop to emit a typed 1001 close frame and
// tear down the connection (Hub.CloseAll path). closeMu guards one-shot
// close of the shutdown and done channels so concurrent Shutdown / handler
// teardown can both run safely.
type connSubscriber struct {
	send chan []byte

	closeMu    sync.Mutex
	done       chan struct{}
	shutdown   chan struct{}
	closeFlush chan struct{}

	userID  string
	channel string
}

func newConnSubscriber(userID, channel string) *connSubscriber {
	return &connSubscriber{
		send:       make(chan []byte, sendBuffer),
		done:       make(chan struct{}),
		shutdown:   make(chan struct{}),
		closeFlush: make(chan struct{}),
		userID:     userID,
		channel:    channel,
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
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}

// Shutdown signals the writeLoop to emit a 1001 close frame and waits
// for the flush (or ctx) before returning. Idempotent: subsequent calls
// observe the already-closed shutdown channel and return immediately
// once the flush completes.
//
// Implements hub.ShutdownSubscriber so Hub.CloseAll can drain every
// open WS subscriber on SIGTERM (issue #788).
func (c *connSubscriber) Shutdown(ctx context.Context) {
	c.closeMu.Lock()
	select {
	case <-c.shutdown:
		// already signalled; fall through to wait on closeFlush.
	default:
		close(c.shutdown)
	}
	c.closeMu.Unlock()

	select {
	case <-c.closeFlush:
	case <-ctx.Done():
	}
}

// signalCloseFlushed marks the close-frame flush done so any goroutine
// blocked in Shutdown returns. One-shot, idempotent.
func (c *connSubscriber) signalCloseFlushed() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	select {
	case <-c.closeFlush:
	default:
		close(c.closeFlush)
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
// for the unit tests in this package that exercise the hub fan-out
// without standing up the auth stack; production wiring always supplies
// a non-nil ts.
//
// Same-origin enforcement is delegated to coder/websocket.Accept,
// which compares Host to Origin by default and additionally honors
// any patterns in cfg.OriginPatterns. A mismatch yields HTTP 403.
//
// Each connection subscribes to one channel for its lifetime: the
// upper-folded ULID supplied via ?channel=<id>. ?channel= is required
// when cfg.ChannelLookup is wired (production); the test-only no-lookup
// path falls back to testDefaultChannel when the param is absent.
func Handler(h *hub.Hub, ts *auth.TicketStore, cfg Config) http.HandlerFunc {
	acceptOpts := &websocket.AcceptOptions{
		OriginPatterns: cfg.OriginPatterns,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("channel")
		var channel string
		if raw == "" {
			if cfg.ChannelLookup != nil {
				http.Error(w, "channel parameter required", http.StatusBadRequest)
				return
			}
			channel = testDefaultChannel
		} else {
			// Cap the channel-key length — a 1 MB query string would
			// otherwise sit in the subscriber map for the lifetime of
			// the connection. 64 chars covers a 26-char ULID plus
			// padding.
			if len(raw) > 64 {
				http.Error(w, "channel parameter too long", http.StatusBadRequest)
				return
			}
			// Upper-fold the channel id so a lower-cased URL hits the
			// same channel as the REST surface, which also folds via
			// ids.NormalizeChannelID (audit #78, info).
			norm, ok := ids.NormalizeChannelID(raw)
			if !ok {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			channel = norm
		}

		// Reject upgrades for unknown channels with HTTP 404 BEFORE the
		// WebSocket handshake — and BEFORE redeeming the ws-ticket, so a
		// typo or probe does not burn the one-shot ticket (audit #78,
		// low-severity). Use http.Error (text/plain) — the JSON envelope
		// lives in internal/http and importing it here would create a
		// cycle (the http package's ws_broadcast_test.go imports wsapi).
		if cfg.ChannelLookup != nil {
			ok, err := cfg.ChannelLookup(r.Context(), channel)
			if err != nil {
				slog.Error("ws channel lookup", "channel", channel, "err", err)
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
			// Ungraceful disconnect (browser tab close, network drop,
			// process kill) bubbles up as io.EOF or "use of closed network
			// connection". The peer didn't send a close frame so the
			// CloseError check above didn't catch it, but it's expected
			// behavior — log at Debug level so prod logs aren't noisy
			// on every page navigation.
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				slog.Debug("ws read: peer disconnected without close frame", "err", err)
				return
			}
			slog.Warn("ws read", "err", err)
			return
		}
		if len(data) > MessageBodyLimit {
			writeErrorFrame(ctx, conn, ErrCodeBodyTooLarge, wsproto.MessageBodyLimitCloseReason)
			_ = conn.Close(websocket.StatusMessageTooBig, wsproto.MessageBodyLimitCloseReason)
			return
		}
		if !bucket.allow() {
			writeErrorFrame(ctx, conn, ErrCodeRateLimited, wsproto.SendRateLimitCloseReason)
			_ = conn.Close(websocket.StatusPolicyViolation, wsproto.SendRateLimitCloseReason)
			return
		}
		// Audit #78 (medium): drop inbound frames silently. An earlier
		// raw-rebroadcast contract let any peer forge {type,data}
		// envelopes with arbitrary sender_user_id, bypassing persistence
		// and the audit log. Producers must use POST
		// /api/channels/{id}/messages so the server attributes the sender
		// from the JWT and persists first. The read still happens (drains
		// the buffer; enforces size + rate limits above) — only the
		// broadcast is gone.
	}
}

func writeLoop(ctx context.Context, conn *websocket.Conn, sub *connSubscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.done:
			return
		case <-sub.shutdown:
			// Server-initiated graceful shutdown (issue #788). Emit
			// the 1001 close frame so the browser sees a clean
			// "going away" instead of a 1006 abnormal closure, then
			// signal the flush so Hub.CloseAll can return. coder/
			// websocket's Close has its own internal 5s write +
			// 5s read budget; the hub-side 2s drain caps total
			// wall-clock so a misbehaving peer can't hold shutdown.
			//
			// Close races the deferred CloseNow in Handler if the
			// request context cancels concurrently; coder/websocket
			// guards Close with a once-token, so a second call is
			// a benign no-op.
			_ = conn.Close(websocket.StatusGoingAway, wsproto.ServerShutdownCloseReason)
			sub.signalCloseFlushed()
			return
		case msg := <-sub.send:
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
	}
}
