package auth

import (
	"sync"
	"testing"
	"time"
)

// SEC-12: a ticket must redeem at most once. The second Redeem within
// the same TTL must return ok=false.
func TestTicketStoreSingleUse(t *testing.T) {
	s := NewTicketStore()
	tok, _ := s.Issue("user-1")

	uid, ok := s.Redeem(tok)
	if !ok || uid != "user-1" {
		t.Fatalf("first Redeem: got (%q, %v), want (%q, true)", uid, ok, "user-1")
	}
	if uid2, ok2 := s.Redeem(tok); ok2 {
		t.Fatalf("second Redeem: got (%q, true), want ok=false", uid2)
	}
}

// 30s TTL: a ticket older than TicketTTL must not redeem.
func TestTicketStoreExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	s := newTicketStore(func() time.Time { return now })
	tok, _ := s.Issue("user-1")

	now = now.Add(TicketTTL + time.Nanosecond)
	if uid, ok := s.Redeem(tok); ok {
		t.Fatalf("expired Redeem succeeded: uid=%q", uid)
	}
}

// Boundary: an entry redeemed at exactly expiresAt is treated as
// expired (we use !now.Before(expiresAt) — closed-on-the-right).
func TestTicketStoreExpiryBoundaryIsExpired(t *testing.T) {
	now := time.Unix(0, 0)
	s := newTicketStore(func() time.Time { return now })
	tok, exp := s.Issue("user-1")

	now = exp
	if _, ok := s.Redeem(tok); ok {
		t.Fatalf("Redeem at exactly expiresAt should fail")
	}
}

func TestTicketStoreUnknownTicket(t *testing.T) {
	s := NewTicketStore()
	if _, ok := s.Redeem("not-a-ticket"); ok {
		t.Fatalf("unknown ticket should not redeem")
	}
}

func TestTicketStoreIssueProducesUniqueTokens(t *testing.T) {
	s := NewTicketStore()
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		tok, _ := s.Issue("user-x")
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate ticket on iter %d: %q", i, tok)
		}
		seen[tok] = struct{}{}
	}
}

// Concurrent Redeem on the same ticket: exactly one caller must win.
// This is SEC-12's race-condition rider — single-use must hold even
// when two ws-upgrade attempts arrive in the same tick.
func TestTicketStoreConcurrentRedeem(t *testing.T) {
	s := NewTicketStore()
	tok, _ := s.Issue("user-1")

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	winners := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, ok := s.Redeem(tok); ok {
				winners <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(winners)
	count := 0
	for range winners {
		count++
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", count)
	}
}
