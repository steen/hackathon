package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// SEC-5 — the 11th login attempt within 5 min from one source IP must
// be rejected. The default LoginIPConfig (Burst=10, Refill=5min) is
// the source of truth for that AC; this test pins the exact threshold.
func TestIPLimiterRejectsEleventhLoginAttemptWithinFiveMinutes(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginIPConfig()
	cfg.Now = func() time.Time { return now }
	l := NewIPLimiter(cfg)

	for i := 1; i <= 10; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d/10 rejected; expected allowed (LoginIPConfig burst=10)", i)
		}
		now = now.Add(time.Second)
	}
	if l.Allow("1.2.3.4") {
		t.Fatalf("11th login attempt within 5 min from one IP must be rejected (SEC-5)")
	}
}

func TestIPLimiterRefillsOverTime(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginIPConfig()
	cfg.Now = func() time.Time { return now }
	l := NewIPLimiter(cfg)

	for i := 0; i < 10; i++ {
		l.Allow("1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatalf("bucket should be empty")
	}
	// LoginIPConfig refills 10 tokens / 5min = 1 token / 30s.
	now = now.Add(31 * time.Second)
	if !l.Allow("1.2.3.4") {
		t.Fatalf("bucket should have refilled at least one token after 31s")
	}
	if l.Allow("1.2.3.4") {
		t.Fatalf("bucket should be empty again after consuming the refill")
	}
}

func TestIPLimiterIsolatesKeys(t *testing.T) {
	cfg := IPLimiterConfig{Burst: 1, Refill: time.Hour, Capacity: 16}
	l := NewIPLimiter(cfg)
	if !l.Allow("a") {
		t.Fatal("a:1 must allow")
	}
	if l.Allow("a") {
		t.Fatal("a:2 must reject")
	}
	if !l.Allow("b") {
		t.Fatal("b:1 must allow (independent bucket)")
	}
}

func TestIPLimiterEvictsLeastRecentlyUsed(t *testing.T) {
	cfg := IPLimiterConfig{Burst: 1, Refill: time.Hour, Capacity: 2}
	l := NewIPLimiter(cfg)
	l.Allow("a")
	l.Allow("b")
	if got := l.Len(); got != 2 {
		t.Fatalf("len after two keys: got %d want 2", got)
	}
	l.Allow("c")
	if got := l.Len(); got != 2 {
		t.Fatalf("len after eviction: got %d want 2", got)
	}
	// "a" was least-recently-used and should have been evicted, so its
	// bucket starts fresh and the next call allows.
	if !l.Allow("a") {
		t.Fatal("a should be a fresh entry after eviction")
	}
}

func TestIPLimiterAllowsConcurrentAccess(t *testing.T) {
	cfg := IPLimiterConfig{Burst: 1000, Refill: time.Hour, Capacity: 16}
	l := NewIPLimiter(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				l.Allow("shared")
			}
		}()
	}
	wg.Wait()
}

func TestIPLimiterEmptyKeyAlwaysAllowed(t *testing.T) {
	l := NewIPLimiter(IPLimiterConfig{Burst: 1, Refill: time.Hour})
	for i := 0; i < 10; i++ {
		if !l.Allow("") {
			t.Fatalf("empty key should not be tracked: rejected on attempt %d", i)
		}
	}
}
