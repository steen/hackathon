package http

import (
	"database/sql"
	stdhttp "net/http"
	"strconv"
	"time"

	"hackathon/apps/server/internal/ratelimit"
)

// CodeRateLimited is the stable error.code clients branch on for 429s.
const CodeRateLimited = "rate_limited"

// AuthEventRateLimited is written to auth_events whenever an IP hits
// the per-IP token bucket. Per-username rejections share the same kind
// (the row's user_id distinguishes them when present); keeping a
// single kind keeps the audit query simple. The string is plumbed
// through a function so callers don't pull the http package's auth
// helpers as a transitive dependency.
const AuthEventRateLimited = "rate_limited"

// IPRateLimit returns middleware that applies limiter to every request,
// keyed on the source IP. On rejection it writes a 429 with the
// user-safe envelope and (when sink is non-nil) appends one row to the
// audit log so the rejected attempt is observable per the spec ACs.
//
// trustedProxy plumbs through to clientIP so that, when set, the per-IP
// rate-limit bucket keys on the leftmost X-Forwarded-For entry rather
// than the proxy's RemoteAddr — without this the bucket collapses to a
// single key behind a reverse proxy (PRD §9 / §11).
//
// The audit sink intentionally takes plain strings rather than the
// concrete authStore — that keeps the middleware reusable from tests
// and from main without dragging in a *sql.DB.
func IPRateLimit(limiter *ratelimit.IPLimiter, retryAfter time.Duration, sink RateLimitAuditSink, trustedProxy bool) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			ip := clientIP(r, trustedProxy)
			if !limiter.Allow(ip) {
				if sink != nil {
					sink.LogRateLimited(r, "", ip)
				}
				writeRateLimited(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserRateLimit returns middleware that applies limiter keyed on the
// authenticated user id from request context. Must be wrapped *inside*
// the JWT middleware so the user id is populated; without a user id the
// middleware passes through (callers paired with RequireJWT will only
// see this branch in tests that bypass the auth chain).
//
// On rejection it writes a 429 with the standard envelope and a
// Retry-After header, and (when sink is non-nil) appends one row to
// auth_events with the user id set so the rejection is observable in
// the audit log alongside per-IP 429s. trustedProxy plumbs through to
// clientIP so the recorded IP matches the IPRateLimit path's behavior
// when the server runs behind a reverse proxy. PRD §9: per-user
// channel-write limit.
func UserRateLimit(limiter *ratelimit.IPLimiter, retryAfter time.Duration, sink RateLimitAuditSink, trustedProxy bool) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			uid := UserID(r.Context())
			if uid != "" && !limiter.Allow(uid) {
				if sink != nil {
					sink.LogRateLimited(r, uid, clientIP(r, trustedProxy))
				}
				writeRateLimited(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitAuditSink is the narrow audit-log surface the rate-limit
// middleware needs. The auth handlers' store implements it.
type RateLimitAuditSink interface {
	LogRateLimited(r *stdhttp.Request, userID, ip string)
}

// NewRateLimitAuditSink builds a RateLimitAuditSink from a *sql.DB so
// wiring code outside the auth feature (e.g. the channels feature's
// per-user limiter) can share the same auth_events writer without
// reaching into the auth handlers' unexported store.
func NewRateLimitAuditSink(db *sql.DB) RateLimitAuditSink { return newAuthStore(db) }

// writeRateLimited emits the 429 envelope plus a Retry-After header so
// well-behaved clients can back off. retryAfter is rounded up to the
// nearest second per RFC 7231 §7.1.3.
func writeRateLimited(w stdhttp.ResponseWriter, retryAfter time.Duration) {
	if retryAfter > 0 {
		secs := int((retryAfter + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	WriteError(w, stdhttp.StatusTooManyRequests, CodeRateLimited, "too many requests, please try again later")
}
