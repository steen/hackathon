package ratelimit

import (
	"testing"
	"time"
)

func TestUserLimiterBackoffGrowsWithFailures(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginUserConfig()
	cfg.Now = func() time.Time { return now }
	l := NewUserLimiter(cfg)

	// Grace failures (2) carry no delay.
	for i := 0; i < cfg.GraceFailures; i++ {
		l.RegisterFailure("alice")
		if ok, _ := l.Allow("alice"); !ok {
			t.Fatalf("failure #%d within grace window should not gate", i+1)
		}
	}
	// Failure #3 → Step (500ms).
	l.RegisterFailure("alice")
	ok, retry := l.Allow("alice")
	if ok || retry < 500*time.Millisecond {
		t.Fatalf("failure #3 should gate ~500ms; got ok=%v retry=%v", ok, retry)
	}
	// Failure #4 → 2 * Step. Use a fresh observation point past the
	// previous gate so the comparison reflects only the new gate.
	now = now.Add(retry)
	l.RegisterFailure("alice")
	_, retry2 := l.Allow("alice")
	if retry2 <= retry {
		t.Fatalf("backoff should grow: retry1=%v retry2=%v", retry, retry2)
	}
}

func TestUserLimiterBackoffCapsAtMaxDelay(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginUserConfig()
	cfg.Now = func() time.Time { return now }
	l := NewUserLimiter(cfg)
	for i := 0; i < 50; i++ {
		l.RegisterFailure("alice")
	}
	_, retry := l.Allow("alice")
	if retry > cfg.MaxDelay {
		t.Fatalf("retry %v exceeded MaxDelay %v", retry, cfg.MaxDelay)
	}
}

func TestUserLimiterResetClearsBackoff(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginUserConfig()
	cfg.Now = func() time.Time { return now }
	l := NewUserLimiter(cfg)

	for i := 0; i < 10; i++ {
		l.RegisterFailure("alice")
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("alice should be gated after 10 failures")
	}
	l.Reset("alice")
	if ok, _ := l.Allow("alice"); !ok {
		t.Fatal("Reset must clear the gate so a successful login is not penalised")
	}
}

func TestUserLimiterExpiresAfterResetAfterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := LoginUserConfig()
	cfg.Now = func() time.Time { return now }
	l := NewUserLimiter(cfg)
	for i := 0; i < 5; i++ {
		l.RegisterFailure("alice")
	}
	now = now.Add(cfg.ResetAfter + time.Second)
	if ok, _ := l.Allow("alice"); !ok {
		t.Fatal("entry should be expired and forgotten after ResetAfter")
	}
}

func TestUserLimiterIsCaseInsensitive(t *testing.T) {
	cfg := LoginUserConfig()
	now := time.Unix(0, 0)
	cfg.Now = func() time.Time { return now }
	l := NewUserLimiter(cfg)
	for i := 0; i < 5; i++ {
		l.RegisterFailure("Alice")
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("case-only variation must not bypass the per-username backoff")
	}
}

func TestUserLimiterEmptyUsernameAlwaysAllowed(t *testing.T) {
	l := NewUserLimiter(LoginUserConfig())
	if ok, _ := l.Allow(""); !ok {
		t.Fatal("empty username must not be tracked")
	}
	if got := l.RegisterFailure(""); got != 0 {
		t.Fatalf("RegisterFailure(\"\") should be a no-op; got %d", got)
	}
}
