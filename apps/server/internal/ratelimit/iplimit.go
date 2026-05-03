// Package ratelimit holds the per-IP token-bucket limiter and the
// per-username login-failure backoff used to harden /api/auth/login and
// /api/auth/register against brute force / abuse (PRD §9, SEC-5).
//
// Both types are concurrency-safe and clock-injectable so tests can
// exercise time-based behavior without sleeping.
package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// IPLimiterConfig pins one limiter's behaviour. Burst is the bucket
// capacity (the maximum number of requests an IP can fire back-to-back
// before being throttled). Refill is the duration over which Burst
// tokens are restored — so steady-state allowance is Burst / Refill.
//
// Capacity bounds the LRU of per-IP entries. Once exceeded, the
// least-recently-touched IP is evicted. The default is generous enough
// for friend-scale traffic but small enough that an attacker cycling
// IPs cannot exhaust memory.
type IPLimiterConfig struct {
	Burst    int
	Refill   time.Duration
	Capacity int
	Now      func() time.Time
}

// LoginIPConfig is the shared default for /api/auth/login: 10 attempts per 5
// minutes from one source IP. SEC-5 requires the 11th attempt to be
// rejected, which is what Burst=10 + Refill=5min produces (the bucket
// starts full at 10 and only refills 1 token every 30s).
func LoginIPConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 10, Refill: 5 * time.Minute, Capacity: 4096}
}

// RegisterIPConfig is the shared default for /api/auth/register: 5 attempts
// per 15 minutes from one source IP. PRD §9.
func RegisterIPConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 5, Refill: 15 * time.Minute, Capacity: 4096}
}

// IPLimiter is a per-key token-bucket limiter backed by a bounded LRU.
// The LRU prevents an attacker rotating source IPs from growing the
// map without bound (PRD §14 risk: "memory growth from per-IP
// tracking; mitigated by a bounded LRU").
type IPLimiter struct {
	cfg IPLimiterConfig

	mu    sync.Mutex
	order *list.List
	index map[string]*list.Element
}

type ipEntry struct {
	key    string
	tokens float64
	last   time.Time
}

// NewIPLimiter returns a limiter using cfg. Zero-valued fields fall
// back to safe defaults so callers can pass a partial struct.
func NewIPLimiter(cfg IPLimiterConfig) *IPLimiter {
	if cfg.Burst <= 0 {
		cfg.Burst = 10
	}
	if cfg.Refill <= 0 {
		cfg.Refill = 5 * time.Minute
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 4096
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &IPLimiter{
		cfg:   cfg,
		order: list.New(),
		index: make(map[string]*list.Element, cfg.Capacity),
	}
}

// Allow consumes one token for key. Returns true when the request is
// permitted, false when the bucket is empty. Calling Allow refreshes
// the LRU position; full buckets are also tracked so an attacker
// flooding a single IP cannot evict legitimate entries.
func (l *IPLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.cfg.Now()
	burst := float64(l.cfg.Burst)
	perSec := burst / l.cfg.Refill.Seconds()

	if elem, ok := l.index[key]; ok {
		e := elem.Value.(*ipEntry)
		elapsed := now.Sub(e.last).Seconds()
		if elapsed > 0 {
			e.tokens += elapsed * perSec
			if e.tokens > burst {
				e.tokens = burst
			}
		}
		e.last = now
		l.order.MoveToFront(elem)
		if e.tokens < 1 {
			return false
		}
		e.tokens--
		return true
	}

	if l.order.Len() >= l.cfg.Capacity {
		oldest := l.order.Back()
		if oldest != nil {
			l.order.Remove(oldest)
			delete(l.index, oldest.Value.(*ipEntry).key)
		}
	}
	e := &ipEntry{key: key, tokens: burst - 1, last: now}
	l.index[key] = l.order.PushFront(e)
	return true
}

// Len returns the current number of tracked IPs. Test-only window into
// the LRU; callers in production code should not depend on it.
func (l *IPLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}
