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
//
// presence is a server-wide reference count of authenticated user IDs
// with at least one open connection. The map is keyed by user ID, the
// value is the number of distinct connections currently held — when
// the count drops to zero the entry is removed so OnlineUserIDs
// reflects the live set without filtering.
type Hub struct {
	mu       sync.RWMutex
	channels map[string]map[Subscriber]struct{}
	presence map[string]int
}

// New returns an empty Hub.
func New() *Hub {
	return &Hub{
		channels: make(map[string]map[Subscriber]struct{}),
		presence: make(map[string]int),
	}
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

// SnapshotSubscribers returns a slice copy of every Subscriber currently
// registered for channel. Intended for tests and debug-only inspection
// (e.g. assertions that the WS handler bound the right per-conn state);
// production code should use Broadcast, not iterate the snapshot.
func (h *Hub) SnapshotSubscribers(channel string) []Subscriber {
	h.mu.RLock()
	defer h.mu.RUnlock()
	subs := h.channels[channel]
	out := make([]Subscriber, 0, len(subs))
	for s := range subs {
		out = append(out, s)
	}
	return out
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

// BroadcastAll delivers msg to every subscriber across every channel.
// Used for presence events (join/leave) which are not scoped to a
// single channel — a user joining is interesting to anyone watching
// any channel. The set is deduplicated so a subscriber that holds
// memberships in multiple channels still receives the event once.
func (h *Hub) BroadcastAll(msg []byte) {
	h.mu.RLock()
	seen := make(map[Subscriber]struct{})
	for _, subs := range h.channels {
		for s := range subs {
			seen[s] = struct{}{}
		}
	}
	targets := make([]Subscriber, 0, len(seen))
	for s := range seen {
		targets = append(targets, s)
	}
	h.mu.RUnlock()
	for _, s := range targets {
		s.Send(msg)
	}
}

// AddPresence records one connection for userID and reports whether
// this is the first connection for that user (the caller will then
// emit a presence "join" event). Empty userID is a no-op returning
// false — phase-0 boot paths run without auth and have no user to
// track.
func (h *Hub) AddPresence(userID string) bool {
	if userID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.presence[userID]
	h.presence[userID] = prev + 1
	return prev == 0
}

// RemovePresence drops one connection for userID and reports whether
// this was the last connection (the caller will then emit a presence
// "leave" event). Removing an unknown userID or an empty string is a
// no-op returning false.
func (h *Hub) RemovePresence(userID string) bool {
	if userID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	count, ok := h.presence[userID]
	if !ok {
		return false
	}
	if count <= 1 {
		delete(h.presence, userID)
		return true
	}
	h.presence[userID] = count - 1
	return false
}

// OnlineUserIDs returns a snapshot of every user ID with ≥1 active
// connection. Order is unspecified; callers that need stable output
// (e.g. a JSON list) should sort the result themselves.
func (h *Hub) OnlineUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.presence))
	for id := range h.presence {
		out = append(out, id)
	}
	return out
}

// PresenceCount returns the number of distinct users currently online.
// Useful for tests and observability without leaking IDs.
func (h *Hub) PresenceCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.presence)
}
