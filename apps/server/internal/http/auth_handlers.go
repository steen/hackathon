package http

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	stdhttp "net/http"
	"regexp"
	"strings"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/ratelimit"
)

// auth_events kinds — kept here as constants so test code can assert
// against the same strings the handlers write.
const (
	AuthEventRegister       = "register"
	AuthEventRegisterFailed = "register_failed"
	AuthEventLoginSuccess   = "login_success"
	AuthEventLoginFailure   = "login_failure"
	AuthEventLogout         = "logout"
	AuthEventTicketIssued   = "ws_ticket_issued"
)

// usernameRe is the validation regex for new usernames. PRD §9 does
// not pin a specific regex; we pick a friend-group-safe set: 3-32
// chars, ASCII letters + digits + dash + underscore.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

// AuthDeps is everything the auth handlers need wired in. Held as a
// struct so AuthHandlers stays a thin constructor and main can build
// it incrementally.
type AuthDeps struct {
	DB         *sql.DB
	Tickets    *auth.TicketStore
	SigningKey []byte
	InviteCode string
	Now        func() time.Time
	// UserLimiter, when non-nil, gates /api/auth/login on the per-username
	// failure backoff (PRD §9). When nil, the per-username gate is
	// skipped — useful for tests that exercise other code paths.
	UserLimiter *ratelimit.UserLimiter
	// TrustedProxy is the parsed CHAT_TRUSTED_PROXY flag (PRD §9 / §11).
	// When true, the auth audit log and the per-IP rate-limit key honor
	// the leftmost X-Forwarded-For entry. Default (false) trusts only
	// r.RemoteAddr; this is what existing tests assume so the zero value
	// is the safe one.
	TrustedProxy bool
}

// AuthHandlers is the bag of http.HandlerFuncs the auth feature
// exposes. Construct with NewAuthHandlers and wire each field onto
// your mux.
type AuthHandlers struct {
	deps  AuthDeps
	store *authStore
}

// NewAuthHandlers wires the dependency bag. Defaults Now to time.Now
// when unset so production callers do not have to think about clocks.
func NewAuthHandlers(deps AuthDeps) *AuthHandlers {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &AuthHandlers{deps: deps, store: newAuthStore(deps.DB)}
}

// AuditSink returns the audit-log sink the rate-limit middleware can
// pass to IPRateLimit so rejected attempts land in auth_events.
func (h *AuthHandlers) AuditSink() RateLimitAuditSink { return h.store }

// LookupUserInfo is the auth.UserInfoLookup the JWT middleware needs.
// Exposed so main.go can pass it into auth.RequireJWT without
// reaching into the unexported store.
func (h *AuthHandlers) LookupUserInfo(ctx context.Context, userID string) (*auth.UserInfo, error) {
	return h.store.LookupUserByID(ctx, userID)
}

// Register handles POST /api/auth/register.
//
// Validation order is intentional: invite code first (so a brute-force
// attempt against the regex/policy/DB cannot be used to discover that
// registration is open without the invite); then username regex; then
// password policy; then a uniqueness-aware insert. Errors return the
// envelope with codes the CLI/web client can branch on.
func (h *AuthHandlers) Register(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	// Capture the attempted username up-front so every register_failed
	// row carries it, even when the request is rejected before the
	// regex check passes. We TrimSpace so a trailing newline doesn't
	// produce a different audit row from the stored user name; every
	// register_failed branch (including regex-miss) records the
	// trimmed value, which groups retries that differ only by trailing
	// whitespace.
	attempted := strings.TrimSpace(req.Username)
	if h.deps.InviteCode == "" {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "registration is disabled")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.InviteCode), []byte(h.deps.InviteCode)) != 1 {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		WriteError(w, stdhttp.StatusForbidden, CodeForbidden, "invalid invite code")
		return
	}
	username := attempted
	if !usernameRe.MatchString(username) {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
			"username must be 3-32 chars: letters, digits, dash, underscore")
		return
	}
	if err := auth.EnforcePolicy(req.Password); err != nil {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		switch {
		case errors.Is(err, auth.ErrPasswordTooShort):
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"password must be at least 10 characters")
		case errors.Is(err, auth.ErrPasswordTooLong):
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest,
				"password must not exceed 72 bytes")
		default:
			WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid password")
		}
		return
	}
	hash, err := auth.Hash(req.Password)
	if err != nil {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not hash password")
		return
	}
	id := ids.NewULID()
	if err := h.store.CreateUser(r.Context(), id, username, hash, h.deps.Now()); err != nil {
		h.logEvent(r.Context(), "", attempted, AuthEventRegisterFailed, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		if errors.Is(err, ErrUsernameTaken) {
			WriteError(w, stdhttp.StatusConflict, CodeConflict, "username already taken")
			return
		}
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not create user")
		return
	}
	h.logEvent(r.Context(), id, username, AuthEventRegister, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
	tok, err := auth.Issue(h.deps.SigningKey, id, 0, h.deps.Now())
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not issue token")
		return
	}
	WriteOK(w, stdhttp.StatusCreated, map[string]interface{}{
		"token": tok,
		"user": map[string]string{
			"id":       id,
			"username": username,
		},
	})
}

// Login handles POST /api/auth/login.
//
// The success/failure split delegates to auth.AuthenticateLogin so the
// constant-time bcrypt path and SEC-4 byte-identical error message
// stay in one place. We log success/failure to auth_events with the
// caller's IP and UA — auth.AuthenticateLogin collapses both failure
// arms (unknown user, wrong password) into (nil, ErrLogin), so the
// failure event has no user_id (auth_events.user_id is NULLABLE for
// exactly this case).
func (h *AuthHandlers) Login(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, stdhttp.StatusBadRequest, CodeBadRequest, "invalid JSON body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if h.deps.UserLimiter != nil {
		if ok, retry := h.deps.UserLimiter.Allow(username); !ok {
			// Per-username rate-limit rejection: route through
			// LogAuthEvent directly so the username we have in
			// scope makes it into the audit row. The middleware-
			// owned LogRateLimited path passes username="" because
			// it fires before the body is decoded.
			h.logEvent(r.Context(), "", username, AuthEventRateLimited, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
			writeRateLimited(w, retry)
			return
		}
	}
	user, err := auth.AuthenticateLogin(func(u string) (*auth.User, error) {
		return h.store.LookupForLogin(r.Context(), u)
	}, username, req.Password)
	if err != nil {
		if h.deps.UserLimiter != nil {
			h.deps.UserLimiter.RegisterFailure(username)
		}
		// Record the attempted username so unknown-user probes are
		// attributable. user_id stays NULL — auth.AuthenticateLogin
		// collapses both failure arms into the same error so we
		// cannot tell unknown-user from wrong-password here.
		h.logEvent(r.Context(), "", username, AuthEventLoginFailure, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, auth.LoginErrorMessage)
		return
	}
	if h.deps.UserLimiter != nil {
		h.deps.UserLimiter.Reset(username)
	}
	tok, err := auth.Issue(h.deps.SigningKey, user.ID, user.TokenVersion, h.deps.Now())
	if err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not issue token")
		return
	}
	h.logEvent(r.Context(), user.ID, username, AuthEventLoginSuccess, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
	if u, ok := h.lookupUsername(r, user.ID); ok {
		username = u
	}
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{
		"token": tok,
		"user": map[string]string{
			"id":       user.ID,
			"username": username,
		},
	})
}

// Me handles GET /api/auth/me. Must be wrapped in auth.RequireJWT.
func (h *AuthHandlers) Me(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "unauthorized")
		return
	}
	uname, _ := auth.UsernameFromContext(r.Context())
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{
		"user": map[string]string{
			"id":       uid,
			"username": uname,
		},
	})
}

// Logout handles POST /api/auth/logout. Must be wrapped in auth.RequireJWT.
// Bumps users.token_version, invalidating every previously-issued JWT
// for the caller (US-12).
func (h *AuthHandlers) Logout(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "unauthorized")
		return
	}
	if _, err := h.store.IncrementTokenVersion(r.Context(), uid); err != nil {
		WriteError(w, stdhttp.StatusInternalServerError, CodeInternal, "could not invalidate token")
		return
	}
	uname, _ := h.lookupUsername(r, uid)
	h.logEvent(r.Context(), uid, uname, AuthEventLogout, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{"ok": true})
}

// WSTicket handles POST /api/auth/ws-ticket. Must be wrapped in
// auth.RequireJWT. Issues a 30s, single-use ticket bound to the
// caller; redemption happens at the WS upgrade.
func (h *AuthHandlers) WSTicket(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		WriteError(w, stdhttp.StatusMethodNotAllowed, CodeMethodNotAllow, "method not allowed")
		return
	}
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, stdhttp.StatusUnauthorized, CodeUnauthorized, "unauthorized")
		return
	}
	tok, exp := h.deps.Tickets.Issue(uid)
	uname, _ := h.lookupUsername(r, uid)
	h.logEvent(r.Context(), uid, uname, AuthEventTicketIssued, clientIP(r, h.deps.TrustedProxy), r.UserAgent())
	WriteOK(w, stdhttp.StatusOK, map[string]interface{}{
		"ticket":     tok,
		"expires_at": exp.UTC().Format(time.RFC3339Nano),
	})
}

// logEvent writes one auth_events row best-effort but surfaces a write
// failure to the operator log so SEC-13 audit gaps are observable.
// username is the attempted-or-resolved username — pass "" when none
// is in scope (e.g. early validation rejections that never read the
// body successfully). The store maps "" to SQL NULL.
func (h *AuthHandlers) logEvent(ctx context.Context, userID, username, kind, ip, ua string) {
	if err := h.store.LogAuthEvent(ctx, userID, username, kind, ip, ua); err != nil {
		log.Printf("auth_events insert failed kind=%s: %v", kind, err)
	}
}

func (h *AuthHandlers) lookupUsername(r *stdhttp.Request, id string) (string, bool) {
	u, err := h.store.LookupUserByID(r.Context(), id)
	if err != nil || u == nil {
		return "", false
	}
	return u.Username, true
}

// decodeJSON enforces strict decoding: unknown fields are rejected so
// a typo in the client cannot silently bypass a required field check.
// The body cap (PRD §9 SEC-7) is applied globally by httpx.BodyCap.
func decodeJSON(r *stdhttp.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// clientIP extracts the source IP for auth-event audit rows and the
// per-IP rate-limit bucket key. When trustedProxy is true (PRD §9 /
// §11, CHAT_TRUSTED_PROXY=1) and the leftmost X-Forwarded-For entry
// parses as an IP literal, that wins; otherwise we fall back to the
// host portion of r.RemoteAddr. SplitHostPort handles IPv6
// ("[::1]:1234") that a naive LastIndex(":") would mangle.
func clientIP(r *stdhttp.Request, trustedProxy bool) string {
	if trustedProxy {
		if ip := LeftmostForwardedFor(r); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteUnauthorized is the auth.MiddlewareConfig.WriteUnauthorized
// function — exported as a package-level helper so main.go does not
// need to import auth and httpapi separately to wire it.
func WriteUnauthorized(w stdhttp.ResponseWriter, code, message string) {
	WriteError(w, stdhttp.StatusUnauthorized, code, message)
}
