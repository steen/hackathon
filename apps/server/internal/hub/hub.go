// Package hub maintains an in-memory registry of WebSocket subscribers per
// channel and broadcasts messages to every subscriber of a given channel.
package hub

import "sync"

// Subscriber is the receiver side of a connected client. The hub never closes
// Send; the owner of the subscriber (typically the connection's write loop) is
// responsible for that.
type Subscriber struct {
	Send chan []byte
}

// NewSubscriber returns a Subscriber whose Send channel has the given buffer
// size. A non-zero buffer is required for Broadcast's non-blocking send to
// behave as documented.
func NewSubscriber(bufSize int) *Subscriber {
	return &Subscriber{Send: make(chan []byte, bufSize)}
}

// Hub tracks subscribers per channel. The zero value is not usable; call New.
type Hub struct {
	mu       sync.RWMutex
	channels map[string]map[*Subscriber]struct{}
}

// New returns an empty Hub.
func New() *Hub {
	return &Hub{channels: make(map[string]map[*Subscriber]struct{})}
}

// Subscribe registers sub on channel. Calling Subscribe more than once with
// the same sub on the same channel is a no-op.
func (h *Hub) Subscribe(channel string, sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[channel]
	if !ok {
		subs = make(map[*Subscriber]struct{})
		h.channels[channel] = subs
	}
	subs[sub] = struct{}{}
}

// Unsubscribe removes sub from channel. Removing an unknown subscriber is a
// no-op.
func (h *Hub) Unsubscribe(channel string, sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[channel]
	if !ok {
		return
	}
	delete(subs, sub)
	if len(subs) == 0 {
		delete(h.channels, channel)
	}
}

// Broadcast delivers msg to every current subscriber of channel. Delivery is
// non-blocking: if a subscriber's Send buffer is full the message is dropped
// for that subscriber rather than stalling delivery to others.
func (h *Hub) Broadcast(channel string, msg []byte) {
	h.mu.RLock()
	subs := make([]*Subscriber, 0, len(h.channels[channel]))
	for s := range h.channels[channel] {
		subs = append(subs, s)
	}
	h.mu.RUnlock()
	for _, s := range subs {
		select {
		case s.Send <- msg:
		default:
			// Slow subscriber: drop this message rather than stall others.
		}
	}
}

// SubscriberCount returns the number of subscribers on channel.
func (h *Hub) SubscriberCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel])
}
