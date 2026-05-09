package cmd

import (
	"sync"
	"time"
)

// unparseableFrameWarnInterval bounds how often the dm/watch loops emit
// "drop unparseable frame" warnings to stderr. A malformed server can
// produce one bad frame per real frame; without throttling the warning
// drowns the stream.
const unparseableFrameWarnInterval = 5 * time.Second

// warnThrottle emits at most one event per interval. The zero value is
// a usable throttle — first call always fires. now defaults to time.Now
// when nil; tests override it for deterministic timing.
type warnThrottle struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	now      func() time.Time
}

// allow reports whether the caller should emit now. Updates last when
// true. A zero interval means every call fires.
func (w *warnThrottle) allow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	clock := w.now
	if clock == nil {
		clock = time.Now
	}
	t := clock()
	if w.last.IsZero() || w.interval <= 0 || t.Sub(w.last) >= w.interval {
		w.last = t
		return true
	}
	return false
}
