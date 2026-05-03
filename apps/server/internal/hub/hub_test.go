package hub_test

import (
	"testing"
	"time"

	"github.com/jumoel/hackathon/apps/server/internal/hub"
)

func TestAC2_Hub_SubscribeRegistersSubscriberOnChannel(t *testing.T) {
	h := hub.New()
	sub := hub.NewSubscriber(4)

	h.Subscribe("#general", sub)

	if got := h.SubscriberCount("#general"); got != 1 {
		t.Fatalf("after Subscribe, SubscriberCount(#general) = %d, want 1", got)
	}

	// Subscribing the same subscriber a second time must not cause a duplicate
	// delivery on broadcast.
	h.Subscribe("#general", sub)
	if got := h.SubscriberCount("#general"); got != 1 {
		t.Fatalf("after duplicate Subscribe, SubscriberCount(#general) = %d, want 1", got)
	}

	h.Broadcast("#general", []byte("hello"))

	select {
	case msg := <-sub.Send:
		if string(msg) != "hello" {
			t.Fatalf("first delivery = %q, want %q", msg, "hello")
		}
	default:
		t.Fatal("subscriber received no message after Broadcast")
	}

	select {
	case msg := <-sub.Send:
		t.Fatalf("subscriber received duplicate message %q after single Broadcast", msg)
	default:
	}
}

func TestAC2_Hub_UnsubscribeRemovesSubscriber(t *testing.T) {
	h := hub.New()
	sub := hub.NewSubscriber(4)

	h.Subscribe("#general", sub)
	h.Unsubscribe("#general", sub)

	if got := h.SubscriberCount("#general"); got != 0 {
		t.Fatalf("after Unsubscribe, SubscriberCount(#general) = %d, want 0", got)
	}

	h.Broadcast("#general", []byte("ignored"))
	select {
	case msg := <-sub.Send:
		t.Fatalf("unsubscribed subscriber still received %q", msg)
	default:
	}

	// Unsubscribing an unknown subscriber must be a no-op (no panic).
	stranger := hub.NewSubscriber(1)
	h.Unsubscribe("#general", stranger)
	h.Unsubscribe("#nowhere", stranger)
}

func TestAC3_Hub_BroadcastDeliversToAllSubscribersOfChannel(t *testing.T) {
	h := hub.New()
	a := hub.NewSubscriber(4)
	b := hub.NewSubscriber(4)
	c := hub.NewSubscriber(4)
	other := hub.NewSubscriber(4)

	h.Subscribe("#general", a)
	h.Subscribe("#general", b)
	h.Subscribe("#general", c)
	h.Subscribe("#offtopic", other)

	h.Broadcast("#general", []byte("hi all"))

	for name, s := range map[string]*hub.Subscriber{"a": a, "b": b, "c": c} {
		select {
		case msg := <-s.Send:
			if string(msg) != "hi all" {
				t.Fatalf("subscriber %s got %q, want %q", name, msg, "hi all")
			}
		default:
			t.Fatalf("subscriber %s did not receive broadcast", name)
		}
		// Each subscriber must receive the message exactly once.
		select {
		case msg := <-s.Send:
			t.Fatalf("subscriber %s got duplicate %q", name, msg)
		default:
		}
	}

	// Subscriber on a different channel must not receive.
	select {
	case msg := <-other.Send:
		t.Fatalf("subscriber on #offtopic received #general broadcast: %q", msg)
	default:
	}
}

func TestAC3_Hub_BroadcastDoesNotBlockOnSlowSubscriber(t *testing.T) {
	h := hub.New()
	// Slow subscriber: tiny buffer that we deliberately fill before broadcast.
	slow := hub.NewSubscriber(1)
	slow.Send <- []byte("filler")

	fast := hub.NewSubscriber(4)

	h.Subscribe("#general", slow)
	h.Subscribe("#general", fast)

	done := make(chan struct{})
	go func() {
		h.Broadcast("#general", []byte("payload"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Broadcast blocked on slow subscriber for >200ms; expected non-blocking delivery")
	}

	// The fast subscriber must still receive the message.
	select {
	case msg := <-fast.Send:
		if string(msg) != "payload" {
			t.Fatalf("fast subscriber got %q, want %q", msg, "payload")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("fast subscriber did not receive payload")
	}

	// Documented behaviour for the slow subscriber is "drop", not deliver
	// out-of-order: only the original filler should remain in its buffer.
	select {
	case msg := <-slow.Send:
		if string(msg) != "filler" {
			t.Fatalf("slow subscriber buffer head = %q, want pre-existing filler", msg)
		}
	default:
		t.Fatal("slow subscriber buffer unexpectedly empty")
	}
	select {
	case msg := <-slow.Send:
		t.Fatalf("slow subscriber received undocumented extra message %q (expected drop)", msg)
	default:
	}
}
