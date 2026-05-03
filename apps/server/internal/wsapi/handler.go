// Package wsapi exposes the /ws HTTP handler that bridges WebSocket
// connections to the in-memory hub.
package wsapi

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/coder/websocket"

	"hackathon/apps/server/internal/hub"
)

const (
	defaultChannel = "#general"
	sendBuffer     = 64

	// ReadLimitBytes caps a single inbound WS frame (PRD §9, SEC-6).
	// Hitting this causes the library to close with StatusMessageTooBig (1009).
	ReadLimitBytes int64 = 64 * 1024

	// SendRateBurst / SendRatePerSec implement the per-connection send
	// rate limit from PRD §9: 10 msg/s, burst 30. Excess inbound frames
	// drop the connection with StatusPolicyViolation (1008).
	SendRateBurst  = 30
	SendRatePerSec = 10.0

	// MessageBodyLimit caps the decoded chat-message body (PRD §9, SEC-8).
	// Mirrors httpx.MessageBodyLimit; the WS path enforces it independently
	// so wsapi has no HTTP-side dependency.
	MessageBodyLimit = 4 * 1024
)

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

// Handler returns an http.HandlerFunc serving the /ws endpoint. Every
// accepted connection subscribes to defaultChannel for the duration of
// its lifetime.
func Handler(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Default Accept rejects mismatched Origin (CSWSH defense). For
		// non-default origins, use OriginPatterns with an explicit
		// allowlist — never InsecureSkipVerify in this code path.
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(ReadLimitBytes)

		sub := newConnSubscriber()
		h.Subscribe(defaultChannel, sub)
		defer h.Unsubscribe(defaultChannel, sub)
		defer sub.close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go writeLoop(ctx, conn, sub)
		readLoop(ctx, conn, h, defaultChannel, newTokenBucket(SendRateBurst, SendRatePerSec))
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
		if !bucket.allow() {
			_ = conn.Close(websocket.StatusPolicyViolation, "send rate limit exceeded")
			return
		}
		if len(data) > MessageBodyLimit {
			_ = conn.Close(websocket.StatusMessageTooBig, "message body exceeds 4 KiB limit")
			return
		}
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
