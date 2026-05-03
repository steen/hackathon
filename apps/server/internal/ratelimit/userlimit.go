package ratelimit

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

// UserLimiterConfig controls the per-username login-failure backoff.
// Step is the linear delay added per failure beyond GraceFailures
// (e.g. Step=500ms with GraceFailures=2 means failure #3 yields a 0.5s
// gate, #4 → 1s, #5 → 1.5s … capped at MaxDelay). MaxDelay caps the
// per-attempt gate so a forgotten password cannot lock an account out
// indefinitely (PRD §9: "linear backoff up to ~2s … without enabling
// lockout-DoS").
//
// Capacity bounds the LRU of tracked usernames so a register-spam
// attacker cannot grow the map. ResetAfter drops a username's record
// once that long has passed since the last failure (so casual
// bad-day-typing doesn't accumulate forever).
type UserLimiterConfig struct {
	Step          time.Duration
	MaxDelay      time.Duration
	GraceFailures int
	Capacity      int
	ResetAfter    time.Duration
	Now           func() time.Time
}

// LoginUserConfig is the shared default for the per-username login
// backoff: 2 free attempts, then linear 500ms steps capped at 2s, with
// records evicted 5 minutes after the last failure.
func LoginUserConfig() UserLimiterConfig {
	return UserLimiterConfig{
		Step:          500 * time.Millisecond,
		MaxDelay:      2 * time.Second,
		GraceFailures: 2,
		Capacity:      4096,
		ResetAfter:    5 * time.Minute,
	}
}

// UserLimiter tracks per-username failure counts and the next time the
// username may attempt login again.
type UserLimiter struct {
	cfg UserLimiterConfig

	mu    sync.Mutex
	order *list.List
	index map[string]*list.Element
}

type userEntry struct {
	key       string
	failures  int
	nextAt    time.Time
	updatedAt time.Time
}

// NewUserLimiter constructs a per-username backoff tracker with sensible
// defaults for any zero-valued cfg field (Step 500ms, MaxDelay 2s,
// GraceFailures 0, ResetAfter 5m, Now time.Now).
func NewUserLimiter(cfg UserLimiterConfig) *UserLimiter {
	if cfg.Step <= 0 {
		cfg.Step = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 2 * time.Second
	}
	if cfg.GraceFailures < 0 {
		cfg.GraceFailures = 0
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 4096
	}
	if cfg.ResetAfter <= 0 {
		cfg.ResetAfter = 5 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &UserLimiter{
		cfg:   cfg,
		order: list.New(),
		index: make(map[string]*list.Element, cfg.Capacity),
	}
}

// normKey folds usernames to lower-case so an attacker cannot bypass
// the per-username backoff by varying letter case across attempts.
func normKey(username string) string { return strings.ToLower(username) }

// Allow reports whether a login attempt for username is currently
// permitted. When false, retryAfter is the duration the caller should
// surface to the client (so a Retry-After header / log line is honest).
func (l *UserLimiter) Allow(username string) (ok bool, retryAfter time.Duration) {
	key := normKey(username)
	if key == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.cfg.Now()
	elem, hit := l.index[key]
	if !hit {
		return true, 0
	}
	e := elem.Value.(*userEntry)
	if now.Sub(e.updatedAt) >= l.cfg.ResetAfter {
		l.order.Remove(elem)
		delete(l.index, key)
		return true, 0
	}
	if now.Before(e.nextAt) {
		return false, e.nextAt.Sub(now)
	}
	return true, 0
}

// RegisterFailure records a failed attempt for username and returns the
// new total failure count. The next-allowed-at gate is bumped by the
// linear backoff defined in cfg.
func (l *UserLimiter) RegisterFailure(username string) int {
	key := normKey(username)
	if key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.cfg.Now()
	var e *userEntry
	if elem, ok := l.index[key]; ok {
		e = elem.Value.(*userEntry)
		l.order.MoveToFront(elem)
		if now.Sub(e.updatedAt) >= l.cfg.ResetAfter {
			e.failures = 0
		}
	} else {
		if l.order.Len() >= l.cfg.Capacity {
			oldest := l.order.Back()
			if oldest != nil {
				l.order.Remove(oldest)
				delete(l.index, oldest.Value.(*userEntry).key)
			}
		}
		e = &userEntry{key: key}
		l.index[key] = l.order.PushFront(e)
	}
	e.failures++
	e.updatedAt = now
	delay := l.delayFor(e.failures)
	e.nextAt = now.Add(delay)
	return e.failures
}

// Reset drops any failure record for username. Call from the success
// path so a legitimate user returning after a typo isn't penalised.
func (l *UserLimiter) Reset(username string) {
	key := normKey(username)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if elem, ok := l.index[key]; ok {
		l.order.Remove(elem)
		delete(l.index, key)
	}
}

// Len returns the current number of tracked usernames; test-only.
func (l *UserLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}

func (l *UserLimiter) delayFor(failures int) time.Duration {
	over := failures - l.cfg.GraceFailures
	if over <= 0 {
		return 0
	}
	d := time.Duration(over) * l.cfg.Step
	if d > l.cfg.MaxDelay {
		d = l.cfg.MaxDelay
	}
	return d
}
