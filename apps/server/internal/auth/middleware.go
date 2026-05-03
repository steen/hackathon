package auth

import (
	"context"
	"net/http"
	"strings"
)

// ctxKey is unexported so middleware-scoped values cannot collide with
// foreign keys placed on the same context.
type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
	ctxKeyUsername
)

// UserInfoLookup loads the per-user fields the middleware needs to
// validate a token. Returning (nil, nil) means "no such user"; an
// error return is reserved for I/O failure. The middleware translates
// "no such user" to 401 the same as a bad signature so a deleted-user
// token cannot leak its existence via a different status code. The
// ctx is the request context so a cancelled request stops the DB
// round-trip.
type UserInfoLookup func(ctx context.Context, userID string) (*UserInfo, error)

// UserInfo is the shape the middleware needs and the request context
// returns. Username is included so handlers can render it without a
// second DB read.
type UserInfo struct {
	ID           string
	Username     string
	TokenVersion int
}

// MiddlewareConfig is the set of dependencies RequireJWT needs. Held as
// a struct rather than positional args so the call site stays readable
// when the auth plumbing grows (e.g. logger, metrics).
type MiddlewareConfig struct {
	SigningKey []byte
	Lookup     UserInfoLookup
	// WriteUnauthorized writes the 401 envelope. Injected so this
	// package does not import the http envelope package (which would
	// create a cycle: auth → httpapi → auth).
	WriteUnauthorized func(w http.ResponseWriter, code, message string)
	// WithUserID, if non-nil, is called with the request context and
	// the resolved user ID after a successful JWT check. The returned
	// context is plumbed into the downstream handler. Injected (rather
	// than this package importing http) so the http package's access
	// log can read the same ctx key its WithUserID writes — without
	// reintroducing the auth → http import cycle. Callers that don't
	// need access-log attribution may leave this nil.
	WithUserID func(ctx context.Context, userID string) context.Context
}

// RequireJWT returns a middleware that admits requests carrying a
// valid `Authorization: Bearer <jwt>` header. On success the request
// context is decorated with the user's ID and username; on any failure
// (missing header, malformed, bad signature, expired, tv mismatch, or
// the user no longer exists) the request is rejected with 401 and the
// downstream handler is never called.
func RequireJWT(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				cfg.WriteUnauthorized(w, "unauthorized", "missing or malformed Authorization header")
				return
			}
			// Two-phase validation: parse without tv check first to extract
			// the subject, then load the user row, then re-parse with the
			// caller's current tv. Splitting it this way means a deleted
			// user (Lookup returns nil, nil) reads identically to a bad
			// signature — both paths give 401 with the same body.
			sub, err := unverifiedSubject(tok)
			if err != nil {
				cfg.WriteUnauthorized(w, "unauthorized", "invalid token")
				return
			}
			user, err := cfg.Lookup(r.Context(), sub)
			if err != nil {
				cfg.WriteUnauthorized(w, "unauthorized", "invalid token")
				return
			}
			if user == nil {
				cfg.WriteUnauthorized(w, "unauthorized", "invalid token")
				return
			}
			if _, err := Parse(cfg.SigningKey, tok, user.TokenVersion); err != nil {
				cfg.WriteUnauthorized(w, "unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUserID, user.ID)
			ctx = context.WithValue(ctx, ctxKeyUsername, user.Username)
			if cfg.WithUserID != nil {
				ctx = cfg.WithUserID(ctx, user.ID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the token portion of an `Authorization: Bearer
// <token>` header. The scheme match is case-insensitive (RFC 7235
// §2.1) and surrounding whitespace is tolerated.
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	h := strings.TrimSpace(header)
	if len(h) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// UserIDFromContext returns the authenticated user's ID for a request
// that has passed RequireJWT. Returns ("", false) if the context was
// not produced by RequireJWT.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUserID).(string)
	return v, ok
}

// UsernameFromContext mirrors UserIDFromContext for the username.
func UsernameFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUsername).(string)
	return v, ok
}
