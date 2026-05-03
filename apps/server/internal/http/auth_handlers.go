package http

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	stdhttp "net/http"
	"regexp"
	"strings"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/ids"
)

// auth_events kinds — kept here as constants so test code can assert
// against the same strings the handlers write.
const (
	AuthEventRegister      = "register"
	AuthEventLoginSuccess  = "login_success"
	AuthEventLoginFailure  = "login_failure"
	AuthEventLogout        = "logout"
	AuthEventTicketIssued  = "ws_ticket_issued"
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

// LookupUserInfo is the auth.UserInfoLookup the JWT middleware needs.
// Exposed so main.go can pass it into auth.RequireJWT without
// reaching into the unexported store.
func (h *AuthHandlers) LookupUserInfo(ctx context.Context, userID string) (*auth.UserInfo, error) {
	return h.store.LookupUserByID(ctx, userID)
}

// Register handles POST /api/register.
//
// Validation order is intentional: invite code first (so a brute-force
// attempt against the regex/policy/DB cannot be used to discover that
// registration is open without the invite); then username regex; then
// password policy; then a uniqueness-aware insert. Errors return the
// envelope with codes the CLI/web client can branch on.
func (h *AuthHandlers) Register(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, CodeMethodNotAllow, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, CodeBadRequest, "invalid JSON body", stdhttp.StatusBadRequest)
		return
	}
	if h.deps.InviteCode == "" {
		WriteError(w, CodeForbidden, "registration is disabled", stdhttp.StatusForbidden)
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.InviteCode), []byte(h.deps.InviteCode)) != 1 {
		WriteError(w, CodeForbidden, "invalid invite code", stdhttp.StatusForbidden)
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernameRe.MatchString(username) {
		WriteError(w, CodeBadRequest, "username must be 3-32 chars: letters, digits, dash, underscore", stdhttp.StatusBadRequest)
		return
	}
	if err := auth.EnforcePolicy(req.Password); err != nil {
		switch {
		case errors.Is(err, auth.ErrPasswordTooShort):
			WriteError(w, CodeBadRequest, "password must be at least 10 characters", stdhttp.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordTooLong):
			WriteError(w, CodeBadRequest, "password must not exceed 72 bytes", stdhttp.StatusBadRequest)
		default:
			WriteError(w, CodeBadRequest, "invalid password", stdhttp.StatusBadRequest)
		}
		return
	}
	hash, err := auth.Hash(req.Password)
	if err != nil {
		WriteError(w, CodeInternal, "could not hash password", stdhttp.StatusInternalServerError)
		return
	}
	id := ids.NewULID()
	if err := h.store.CreateUser(r.Context(), id, username, hash, h.deps.Now()); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			WriteError(w, CodeConflict, "username already taken", stdhttp.StatusConflict)
			return
		}
		WriteError(w, CodeInternal, "could not create user", stdhttp.StatusInternalServerError)
		return
	}
	_ = h.store.LogAuthEvent(r.Context(), id, AuthEventRegister, clientIP(r), r.UserAgent())
	tok, err := auth.Issue(h.deps.SigningKey, id, 0, h.deps.Now())
	if err != nil {
		WriteError(w, CodeInternal, "could not issue token", stdhttp.StatusInternalServerError)
		return
	}
	WriteOKStatus(w, stdhttp.StatusCreated, map[string]interface{}{
		"token": tok,
		"user": map[string]string{
			"id":       id,
			"username": username,
		},
	})
}

// Login handles POST /api/login.
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
		WriteError(w, CodeMethodNotAllow, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, CodeBadRequest, "invalid JSON body", stdhttp.StatusBadRequest)
		return
	}
	user, err := auth.AuthenticateLogin(func(username string) (*auth.User, error) {
		return h.store.LookupForLogin(r.Context(), username)
	}, req.Username, req.Password)
	if err != nil {
		_ = h.store.LogAuthEvent(r.Context(), "", AuthEventLoginFailure, clientIP(r), r.UserAgent())
		WriteError(w, CodeUnauthorized, auth.LoginErrorMessage, stdhttp.StatusUnauthorized)
		return
	}
	tok, err := auth.Issue(h.deps.SigningKey, user.ID, user.TokenVersion, h.deps.Now())
	if err != nil {
		WriteError(w, CodeInternal, "could not issue token", stdhttp.StatusInternalServerError)
		return
	}
	_ = h.store.LogAuthEvent(r.Context(), user.ID, AuthEventLoginSuccess, clientIP(r), r.UserAgent())
	username, _ := h.lookupUsername(r, user.ID)
	WriteOK(w, map[string]interface{}{
		"token": tok,
		"user": map[string]string{
			"id":       user.ID,
			"username": username,
		},
	})
}

// Me handles GET /api/me. Must be wrapped in auth.RequireJWT.
func (h *AuthHandlers) Me(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		WriteError(w, CodeMethodNotAllow, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}
	uid, _ := auth.UserIDFromContext(r.Context())
	uname, _ := auth.UsernameFromContext(r.Context())
	WriteOK(w, map[string]interface{}{
		"user": map[string]string{
			"id":       uid,
			"username": uname,
		},
	})
}

// Logout handles POST /api/logout. Must be wrapped in auth.RequireJWT.
// Bumps users.token_version, invalidating every previously-issued JWT
// for the caller (US-12).
func (h *AuthHandlers) Logout(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, CodeMethodNotAllow, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}
	uid, _ := auth.UserIDFromContext(r.Context())
	if _, err := h.store.IncrementTokenVersion(r.Context(), uid); err != nil {
		WriteError(w, CodeInternal, "could not invalidate token", stdhttp.StatusInternalServerError)
		return
	}
	_ = h.store.LogAuthEvent(r.Context(), uid, AuthEventLogout, clientIP(r), r.UserAgent())
	WriteOK(w, map[string]interface{}{"ok": true})
}

// WSTicket handles POST /api/ws-ticket. Must be wrapped in
// auth.RequireJWT. Issues a 30s, single-use ticket bound to the
// caller; redemption happens at the WS upgrade.
func (h *AuthHandlers) WSTicket(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		WriteError(w, CodeMethodNotAllow, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}
	uid, _ := auth.UserIDFromContext(r.Context())
	tok, exp := h.deps.Tickets.Issue(uid)
	_ = h.store.LogAuthEvent(r.Context(), uid, AuthEventTicketIssued, clientIP(r), r.UserAgent())
	WriteOK(w, map[string]interface{}{
		"ticket":     tok,
		"expires_at": exp.UTC().Format(time.RFC3339Nano),
	})
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
// MaxBytesReader caps the body at 16 KiB per PRD §9 (the body-and-ws
// caps feature will hoist this into shared middleware later).
func decodeJSON(r *stdhttp.Request, dst interface{}) error {
	r.Body = stdhttp.MaxBytesReader(nil, r.Body, 16*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// clientIP extracts the source IP for the access log. For now we trust
// only RemoteAddr; honoring X-Forwarded-For is gated on
// CHAT_TRUSTED_PROXY (PRD §9) and lands with the rate-limit feature.
// SplitHostPort handles IPv6 ("[::1]:1234") that a naive LastIndex(":")
// would mangle.
func clientIP(r *stdhttp.Request) string {
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
	WriteError(w, code, message, stdhttp.StatusUnauthorized)
}
