package hub

import (
	"sort"
	"sync"
	"testing"
)

func TestAddPresenceFirstReturnsTrue(t *testing.T) {
	h := New()
	if first := h.AddPresence("u1"); !first {
		t.Fatalf("first connection for u1 should return true")
	}
	if first := h.AddPresence("u1"); first {
		t.Fatalf("second connection for u1 should return false")
	}
}

func TestRemovePresenceLastReturnsTrue(t *testing.T) {
	h := New()
	h.AddPresence("u1")
	h.AddPresence("u1")
	if last := h.RemovePresence("u1"); last {
		t.Fatalf("removing 1 of 2 connections should return false")
	}
	if last := h.RemovePresence("u1"); !last {
		t.Fatalf("removing the last connection should return true")
	}
}

func TestRemovePresenceUnknownIsNoop(t *testing.T) {
	h := New()
	if last := h.RemovePresence("ghost"); last {
		t.Fatalf("removing unknown user must not report 'last'")
	}
}

func TestPresenceEmptyUserIDIgnored(t *testing.T) {
	h := New()
	if first := h.AddPresence(""); first {
		t.Fatalf("empty userID must be a no-op (got first=true)")
	}
	if last := h.RemovePresence(""); last {
		t.Fatalf("empty userID remove must be a no-op (got last=true)")
	}
	if h.PresenceCount() != 0 {
		t.Fatalf("empty userID must not occupy a presence slot")
	}
}

func TestOnlineUserIDsReflectsLiveSet(t *testing.T) {
	h := New()
	h.AddPresence("u1")
	h.AddPresence("u2")
	h.AddPresence("u1")
	got := h.OnlineUserIDs()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("OnlineUserIDs = %v, want [u1 u2]", got)
	}
	h.RemovePresence("u1")
	if got := h.OnlineUserIDs(); len(got) != 2 {
		t.Fatalf("u1 still has 1 connection, should still be online: %v", got)
	}
	h.RemovePresence("u1")
	got = h.OnlineUserIDs()
	if len(got) != 1 || got[0] != "u2" {
		t.Fatalf("after u1 fully gone, OnlineUserIDs = %v, want [u2]", got)
	}
}

func TestBroadcastAllReachesEverySubscriberOnce(t *testing.T) {
	h := New()
	a, b, c := &recorder{}, &recorder{}, &recorder{}
	h.Subscribe("#general", a)
	h.Subscribe("#random", b)
	// c is in two channels — must still receive the broadcast exactly once.
	h.Subscribe("#general", c)
	h.Subscribe("#random", c)

	h.BroadcastAll([]byte("ping"))

	for name, r := range map[string]*recorder{"a": a, "b": b, "c": c} {
		got := r.snapshot()
		if len(got) != 1 || string(got[0]) != "ping" {
			t.Fatalf("subscriber %s: want [ping] once, got %v", name, got)
		}
	}
}

func TestBroadcastAllNoSubscribersIsNoop(t *testing.T) {
	h := New()
	h.BroadcastAll([]byte("x"))
}

func TestPresenceConcurrent(t *testing.T) {
	h := New()
	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.AddPresence("u1")
			h.RemovePresence("u1")
		}()
	}
	wg.Wait()
	if got := h.PresenceCount(); got != 0 {
		t.Fatalf("after balanced add/remove, PresenceCount = %d, want 0", got)
	}
}
