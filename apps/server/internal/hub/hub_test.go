package hub

import (
	"sync"
	"testing"
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
