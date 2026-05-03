package http

import (
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
// The audit sink intentionally takes plain strings rather than the
// concrete authStore — that keeps the middleware reusable from tests
// and from main without dragging in a *sql.DB.
func IPRateLimit(limiter *ratelimit.IPLimiter, retryAfter time.Duration, sink RateLimitAuditSink) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			ip := clientIP(r)
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

// RateLimitAuditSink is the narrow audit-log surface the rate-limit
// middleware needs. The auth handlers' store implements it.
type RateLimitAuditSink interface {
	LogRateLimited(r *stdhttp.Request, userID, ip string)
}

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
