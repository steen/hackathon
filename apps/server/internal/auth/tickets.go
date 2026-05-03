package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TicketTTL is the lifetime of a ws-ticket. PRD §9 fixes this at 30s —
// short enough that even leaking a ticket via an access log is bounded
// to a 30-second blast radius.
const TicketTTL = 30 * time.Second

// ticketBytes is the random-byte width before hex encoding. 32 bytes →
// 256 bits → 64 hex chars. Wider than PRD §9's "single-use, 30s ticket"
// minimum on purpose; the cost is one extra read from crypto/rand.
const ticketBytes = 32

// TicketStore is an in-memory map of ticket → user_id with per-ticket
// expiry. Process-restart wipes the store, which is acceptable given
// the 30s TTL: any in-flight ws-ticket would have expired before a
// restart finished.
type TicketStore struct {
	mu      sync.Mutex
	now     func() time.Time
	tickets map[string]ticketEntry
}

type ticketEntry struct {
	userID    string
	expiresAt time.Time
}

// NewTicketStore returns an empty store using time.Now for clock reads.
func NewTicketStore() *TicketStore {
	return newTicketStore(time.Now)
}

// newTicketStore is the test seam for clock injection.
func newTicketStore(now func() time.Time) *TicketStore {
	return &TicketStore{
		now:     now,
		tickets: make(map[string]ticketEntry),
	}
}

// Issue mints a fresh ticket for userID and returns the ticket string
// plus its absolute expiry time. Random bytes come from crypto/rand;
// failure there is treated as fatal by panicking — the caller cannot
// recover meaningfully if the OS RNG is broken.
func (s *TicketStore) Issue(userID string) (string, time.Time) {
	var b [ticketBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	tok := hex.EncodeToString(b[:])
	exp := s.now().Add(TicketTTL)
	s.mu.Lock()
	s.tickets[tok] = ticketEntry{userID: userID, expiresAt: exp}
	s.mu.Unlock()
	return tok, exp
}

// Redeem consumes a ticket. Returns (userID, true) on the first call
// for a live ticket; (zero, false) for unknown, expired, or already-
// consumed tickets. The deletion happens before the expiry check is
// returned so a second call cannot succeed even if the first races
// with another goroutine (SEC-12: single-use).
func (s *TicketStore) Redeem(ticket string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.tickets[ticket]
	if !ok {
		return "", false
	}
	delete(s.tickets, ticket)
	if !s.now().Before(e.expiresAt) {
		return "", false
	}
	return e.userID, true
}
