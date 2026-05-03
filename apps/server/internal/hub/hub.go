// Package hub is an in-memory pub/sub fan-out keyed by channel name.
package hub

import "sync"

// Subscriber receives broadcast payloads on Send. Implementations are
// expected to drop messages when their internal queue is full rather than
// blocking the hub.
type Subscriber interface {
	Send(msg []byte)
}

// Hub is an in-memory pub/sub registry keyed by channel name. Safe for
// concurrent Subscribe/Unsubscribe/Broadcast.
type Hub struct {
	mu       sync.RWMutex
	channels map[string]map[Subscriber]struct{}
}

// New returns an empty Hub.
func New() *Hub {
	return &Hub{channels: make(map[string]map[Subscriber]struct{})}
}

// Subscribe registers s as a receiver for messages broadcast to channel.
func (h *Hub) Subscribe(channel string, s Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[channel]
	if !ok {
		subs = make(map[Subscriber]struct{})
		h.channels[channel] = subs
	}
	subs[s] = struct{}{}
}

// Unsubscribe removes s from channel. Removing an unknown subscriber or an
// unknown channel is a no-op.
func (h *Hub) Unsubscribe(channel string, s Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[channel]
	if !ok {
		return
	}
	delete(subs, s)
	if len(subs) == 0 {
		delete(h.channels, channel)
	}
}

// SubscriberCount returns the number of subscribers on channel. Intended
// primarily for tests and observability.
func (h *Hub) SubscriberCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel])
}

// Broadcast delivers msg to every subscriber of channel. Snapshot the
// subscriber set under the read lock so Send calls do not block hub
// mutations and a slow subscriber cannot stall the hub.
func (h *Hub) Broadcast(channel string, msg []byte) {
	h.mu.RLock()
	subs := h.channels[channel]
	targets := make([]Subscriber, 0, len(subs))
	for s := range subs {
		targets = append(targets, s)
	}
	h.mu.RUnlock()
	for _, s := range targets {
		s.Send(msg)
	}
}
