package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

// EventTypeMessage is the `type` field of the {type:"message",data:<Message>}
// envelope the server emits for new chat messages. Mirrors
// apps/server/internal/http.WSEventMessage.
const EventTypeMessage = "message"

// Event is the typed view of a single inbound WS frame. When the frame
// matches the {type:"message",data:<Message>} shape, Type is set and
// Message is non-nil. Frames that fall outside that shape (e.g. the
// raw phase-0 rebroadcast contract) surface with Type == "" and the
// untouched bytes in Raw, so callers can layer their own decoding
// without losing data.
type Event struct {
	Type    string
	Message *Message
	Raw     []byte
}

// WatchOptions tunes Watch. ChannelID, when set, is forwarded as the
// `?channel=<id>` query parameter on the upgrade — the server uses it
// to pick which hub topic the connection subscribes to. When empty,
// the server falls back to its `#general` default.
type WatchOptions struct {
	ChannelID string
}

// Watch opens a WebSocket subscription and returns a receive-only
// channel of inbound events. The connection lifetime is bound to ctx:
// cancel ctx (or hit a server-side error) and the events channel is
// closed.
//
// Watch handles the ticket dance internally — it calls WsTicket, then
// redeems the ticket as `?ticket=<hex>` on the upgrade. Bearer tokens
// are NOT sent on the upgrade per SEC-12.
func (c *Client) Watch(ctx context.Context, opts WatchOptions) (<-chan Event, error) {
	ticket, err := c.WsTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("mint ws ticket: %w", err)
	}
	wsURL, err := buildWSURL(c.baseURL, ticket.Ticket, opts.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("build ws url: %w", err)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: c.http,
	})
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	// Match wsapi.ReadLimitBytes — frames larger than this would have
	// been truncated server-side anyway, so accepting them here would
	// only buffer attacker-controlled bytes.
	conn.SetReadLimit(64 * 1024)

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		// CloseNow's error is non-actionable here: by the time the loop
		// exits, the underlying TCP connection is being torn down either
		// way (caller cancelled ctx or the server already closed).
		defer func() { _ = conn.CloseNow() }()
		for {
			_, data, readErr := conn.Read(ctx)
			if readErr != nil {
				return
			}
			ev := decodeEvent(data)
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// decodeEvent parses one inbound WS frame. When the bytes match the
// {type:"message",data:<Message>} envelope, Event.Message is populated
// and Type is set; otherwise the raw bytes are surfaced unchanged so
// callers can branch on the older phase-0 raw-rebroadcast contract.
func decodeEvent(data []byte) Event {
	raw := append([]byte(nil), data...)
	var probe struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &probe); err != nil || probe.Type == "" {
		return Event{Raw: raw}
	}
	ev := Event{Type: probe.Type, Raw: raw}
	if probe.Type == EventTypeMessage && len(probe.Data) > 0 {
		var m Message
		if err := json.Unmarshal(probe.Data, &m); err == nil {
			ev.Message = &m
		}
	}
	return ev
}

// buildWSURL converts the REST base URL into the matching WS endpoint,
// appending the ticket and (optional) channel query parameters. The
// scheme is rewritten http→ws / https→wss; any other scheme is passed
// through unchanged so callers using ws:// or wss:// directly still work.
func buildWSURL(base, ticket, channelID string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	q := u.Query()
	q.Set("ticket", ticket)
	if channelID != "" {
		q.Set("channel", channelID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
