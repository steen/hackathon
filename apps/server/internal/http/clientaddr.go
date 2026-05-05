package http

import (
	"net/http"
	"net/netip"
	"strings"
)

// LeftmostForwardedFor returns the leftmost entry of the request's
// X-Forwarded-For header, or the empty string if the header is absent
// or its leftmost entry does not parse as an IP literal.
//
// Validation via netip.ParseAddr is deliberate: an unvalidated value
// would let a client poison the access log (and the per-IP rate-limit
// bucket key) with arbitrary bytes — the trusted-proxy contract is
// "honor what the proxy wrote", not "echo whatever the client sent".
// On any failure mode (empty header, all-whitespace, garbage in the
// leftmost slot) the caller falls back to RemoteAddr.
//
// IPv6 entries are accepted in either bare ("2001:db8::1") or
// bracketed ("[2001:db8::1]") form. Brackets without a port are
// stripped before validation; netip.ParseAddr does not accept
// "[2001:db8::1]" itself, only the unbracketed literal.
//
// This helper does NOT consult any "trust the proxy" flag — that is
// the caller's job. CHAT_TRUSTED_PROXY gating happens one level up,
// in remoteIP / clientIP. Keeping the helper unconditional makes it
// trivially testable and reusable from any middleware that needs the
// same parse + validation.
func LeftmostForwardedFor(r *http.Request) string {
	if r == nil {
		return ""
	}
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		return ""
	}
	// Split on comma; trim ASCII whitespace per RFC 7239 §4.
	first, _, _ := strings.Cut(raw, ",")
	first = strings.TrimSpace(first)
	if first == "" {
		return ""
	}
	// Strip a single matched pair of brackets so "[2001:db8::1]"
	// reaches ParseAddr in its bare form. We do NOT call SplitHostPort
	// here: the leftmost X-Forwarded-For entry is conventionally an
	// IP literal without a port, and accepting "host:port" would
	// silently widen the contract.
	if len(first) >= 2 && first[0] == '[' && first[len(first)-1] == ']' {
		first = first[1 : len(first)-1]
	}
	if _, err := netip.ParseAddr(first); err != nil {
		return ""
	}
	return first
}
