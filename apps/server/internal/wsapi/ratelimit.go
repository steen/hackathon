package wsapi

import (
	"sync"
	"time"
)

// tokenBucket is a single-connection rate limiter. allow() returns false
// when the caller exceeded the configured rate, and the connection
// should close with policy-violation per PRD §9.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	refill   float64
	lastFill time.Time
}

func newTokenBucket(burst int, perSec float64) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		refill:   perSec,
		lastFill: time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.refill
		if b.tokens > b.max {
			b.tokens = b.max
		}
		b.lastFill = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
