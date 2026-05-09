package cmd

import (
	"testing"
	"time"
)

func TestWarnThrottleFirstAlwaysAllows(t *testing.T) {
	w := &warnThrottle{interval: time.Second, now: func() time.Time { return time.Unix(100, 0) }}
	if !w.allow() {
		t.Fatalf("first allow() = false, want true")
	}
}

func TestWarnThrottleSuppressesWithinInterval(t *testing.T) {
	t0 := time.Unix(100, 0)
	now := t0
	w := &warnThrottle{interval: 5 * time.Second, now: func() time.Time { return now }}
	if !w.allow() {
		t.Fatalf("first allow() = false")
	}
	now = t0.Add(4 * time.Second)
	if w.allow() {
		t.Fatalf("allow() = true at +4s, want suppressed (interval=5s)")
	}
	now = t0.Add(5 * time.Second)
	if !w.allow() {
		t.Fatalf("allow() = false at +5s, want fires (interval boundary)")
	}
	now = t0.Add(7 * time.Second)
	if w.allow() {
		t.Fatalf("allow() = true at +7s, want suppressed (last fire was +5s)")
	}
	now = t0.Add(10 * time.Second)
	if !w.allow() {
		t.Fatalf("allow() = false at +10s, want fires")
	}
}

func TestWarnThrottleZeroIntervalAlwaysFires(t *testing.T) {
	now := time.Unix(100, 0)
	w := &warnThrottle{interval: 0, now: func() time.Time { return now }}
	for i := 0; i < 3; i++ {
		if !w.allow() {
			t.Fatalf("allow() #%d = false, want true (zero interval)", i)
		}
	}
}
