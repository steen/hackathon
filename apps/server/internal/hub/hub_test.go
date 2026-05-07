package hub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recorder struct {
	mu  sync.Mutex
	got [][]byte
}

func (r *recorder) Send(msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	r.got = append(r.got, cp)
}

func (r *recorder) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.got))
	copy(out, r.got)
	return out
}

func TestBroadcastReachesAllSubscribers(t *testing.T) {
	h := New()
	a, b := &recorder{}, &recorder{}
	h.Subscribe("#general", a)
	h.Subscribe("#general", b)

	h.Broadcast("#general", []byte("hello"))

	for name, r := range map[string]*recorder{"a": a, "b": b} {
		got := r.snapshot()
		if len(got) != 1 || string(got[0]) != "hello" {
			t.Fatalf("subscriber %s: want [hello], got %v", name, got)
		}
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	h := New()
	a, b := &recorder{}, &recorder{}
	h.Subscribe("#general", a)
	h.Subscribe("#general", b)
	h.Unsubscribe("#general", a)

	h.Broadcast("#general", []byte("after"))

	if got := a.snapshot(); len(got) != 0 {
		t.Fatalf("unsubscribed a should not receive, got %v", got)
	}
	if got := b.snapshot(); len(got) != 1 || string(got[0]) != "after" {
		t.Fatalf("b should receive [after], got %v", got)
	}
}

func TestBroadcastIsolatedPerChannel(t *testing.T) {
	h := New()
	gen, other := &recorder{}, &recorder{}
	h.Subscribe("#general", gen)
	h.Subscribe("#other", other)

	h.Broadcast("#general", []byte("g"))

	if got := other.snapshot(); len(got) != 0 {
		t.Fatalf("#other should not receive #general traffic, got %v", got)
	}
	if got := gen.snapshot(); len(got) != 1 || string(got[0]) != "g" {
		t.Fatalf("#general subscriber should receive [g], got %v", got)
	}
}

func TestBroadcastToUnknownChannelIsNoop(t *testing.T) {
	h := New()
	h.Broadcast("#nobody", []byte("x"))
}

func TestUnsubscribeUnknownIsNoop(t *testing.T) {
	h := New()
	h.Unsubscribe("#nobody", &recorder{})
}

// shutdownRec records Shutdown invocations and optionally blocks for
// `wait` to model a slow flusher. It also implements Send so it satisfies
// hub.Subscriber.
type shutdownRec struct {
	calls atomic.Int32
	wait  time.Duration
}

func (s *shutdownRec) Send(_ []byte) {}

func (s *shutdownRec) Shutdown(ctx context.Context) {
	s.calls.Add(1)
	if s.wait <= 0 {
		return
	}
	select {
	case <-time.After(s.wait):
	case <-ctx.Done():
	}
}

func TestCloseAllInvokesShutdownOnEverySubscriber(t *testing.T) {
	h := New()
	subs := make([]*shutdownRec, 5)
	for i := range subs {
		subs[i] = &shutdownRec{}
	}
	h.Subscribe("#general", subs[0])
	h.Subscribe("#general", subs[1])
	h.Subscribe("#other", subs[2])
	h.Subscribe("#other", subs[3])
	// A subscriber that holds memberships in two channels must still
	// receive exactly one Shutdown call (dedup invariant).
	h.Subscribe("#general", subs[4])
	h.Subscribe("#other", subs[4])

	h.CloseAll()

	for i, s := range subs {
		if got := s.calls.Load(); got != 1 {
			t.Fatalf("subscriber %d: want 1 Shutdown call, got %d", i, got)
		}
	}
}

func TestCloseAllReturnsWithinBudgetWhenSubscriberHangs(t *testing.T) {
	h := New()
	// wait > closeAllDrainBudget — the hub must not block on it.
	slow := &shutdownRec{wait: 10 * time.Second}
	h.Subscribe("#general", slow)

	start := time.Now()
	h.CloseAll()
	elapsed := time.Since(start)

	// Allow generous slack over the 2s budget for slow CI runners but
	// fail loud if the hub waited the full slow.wait.
	if elapsed > closeAllDrainBudget+2*time.Second {
		t.Fatalf("CloseAll exceeded budget: elapsed=%s budget=%s", elapsed, closeAllDrainBudget)
	}
	if got := slow.calls.Load(); got != 1 {
		t.Fatalf("slow subscriber: want 1 Shutdown call, got %d", got)
	}
}

// plainSubscriber implements hub.Subscriber but NOT ShutdownSubscriber;
// CloseAll must skip it without panicking.
type plainSubscriber struct{ recorder }

func TestCloseAllSkipsSubscribersWithoutShutdown(t *testing.T) {
	h := New()
	plain := &plainSubscriber{}
	h.Subscribe("#general", plain)
	// Adding one shutdown-capable subscriber confirms the skip path
	// doesn't break iteration.
	rec := &shutdownRec{}
	h.Subscribe("#general", rec)

	h.CloseAll()

	if got := rec.calls.Load(); got != 1 {
		t.Fatalf("shutdown-capable subscriber: want 1 call, got %d", got)
	}
}

func TestCloseAllNoSubscribersIsNoop(t *testing.T) {
	h := New()
	h.CloseAll()
}
