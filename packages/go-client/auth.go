package goclient

// Wire types — keep in sync with packages/api-client/src/types.ts.
// When adding a JSON field here, mirror it in TS and add an e2e assertion.

import (
	"context"
	"net/http"
	"time"
)

// User mirrors the {id, username} pair the server returns from auth
// endpoints (POST /api/auth/login, POST /api/auth/register, GET /api/auth/me).
//
// BoxPubkey and SignPubkey are Phase-10 identity pubkeys (decision-log
// L2). base64 of raw 32 bytes each. Omitempty under the L26 optional-first
// rule until #4 lands the server population.
type User struct {
	ID         ULID   `json:"id"`
	Username   string `json:"username"`
	BoxPubkey  string `json:"box_pubkey,omitempty"`
	SignPubkey string `json:"sign_pubkey,omitempty"`
}

// AuthResponse is the success payload from POST /api/auth/login and
// POST /api/auth/register: a freshly-issued JWT plus the canonical
// user record. Login() and Register() also call SetToken on the
// Client so callers do not have to wire the token back in by hand.
type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// loginRequest is the wire body for POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// registerRequest mirrors loginRequest plus the invite code the server
// requires per PRD §9.
//
// Phase-10 identity pubkeys (decision-log §4 + L1) ride the same body
// so the server can persist them in the same INSERT that creates the
// row. Both fields are base64 of raw 32 bytes; pre-Phase-10 callers
// that pass empty strings will emit `box_pubkey=""` / `sign_pubkey=""`
// — the server tolerates that until the wave-6 cutover (#983) tightens
// the columns to NOT NULL. JSON tags omit `omitempty` because the
// server's dec.DisallowUnknownFields() decoder accepts the absent-or-
// empty cases identically; new code paths populate them explicitly.
type registerRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
	BoxPubkey  string `json:"box_pubkey,omitempty"`
	SignPubkey string `json:"sign_pubkey,omitempty"`
}

// Login authenticates and stores the resulting bearer token on the
// Client. The same AuthResponse is returned to the caller for
// inspection (e.g. to persist the token to disk).
func (c *Client) Login(ctx context.Context, username, password string) (*AuthResponse, error) {
	var out AuthResponse
	if err := c.do(ctx, http.MethodPost, "/api/auth/login", loginRequest{
		Username: username,
		Password: password,
	}, &out); err != nil {
		return nil, err
	}
	c.SetToken(out.Token)
	return &out, nil
}

// Register creates a new user and stores the resulting bearer token on
// the Client. Mirrors Login's contract. The Phase-10 pubkey fields stay
// empty on this path; new callers should prefer RegisterWithIdentity
// so the user's box_pubkey + sign_pubkey are persisted in the same
// INSERT that creates the row (decision-log §4).
func (c *Client) Register(ctx context.Context, username, password, inviteCode string) (*AuthResponse, error) {
	return c.RegisterWithIdentity(ctx, username, password, inviteCode, "", "")
}

// RegisterWithIdentity is Register plus the two Phase-10 identity
// pubkeys (base64 of raw 32 bytes each). Pass "" for both to land on
// the legacy code path. On success the bearer token is set on the
// Client like Register does.
func (c *Client) RegisterWithIdentity(ctx context.Context, username, password, inviteCode, boxPubkey, signPubkey string) (*AuthResponse, error) {
	var out AuthResponse
	if err := c.do(ctx, http.MethodPost, "/api/auth/register", registerRequest{
		Username:   username,
		Password:   password,
		InviteCode: inviteCode,
		BoxPubkey:  boxPubkey,
		SignPubkey: signPubkey,
	}, &out); err != nil {
		return nil, err
	}
	c.SetToken(out.Token)
	return &out, nil
}

// meResponse mirrors GET /api/auth/me's `{user: {...}}` envelope payload.
type meResponse struct {
	User User `json:"user"`
}

// Me returns the user record for the currently-authenticated bearer.
// Requires Token() to be non-empty (or the call will fail with a 401).
func (c *Client) Me(ctx context.Context) (*User, error) {
	var out meResponse
	if err := c.do(ctx, http.MethodGet, "/api/auth/me", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// Logout invalidates every token previously issued for the caller (the
// server bumps users.token_version). The local Client token is cleared
// on success so subsequent calls do not 401 with a stale credential.
func (c *Client) Logout(ctx context.Context) error {
	if err := c.do(ctx, http.MethodPost, "/api/auth/logout", nil, nil); err != nil {
		return err
	}
	c.SetToken("")
	return nil
}

// WSTicket is the response from POST /api/auth/ws-ticket: a one-shot,
// 30-second token redeemed at the WebSocket upgrade.
type WSTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WsTicket mints a fresh WS ticket. Watch() calls this internally; it
// is exported so callers that build their own upgrade flow (e.g. a
// browser-style EventSource shim) can reuse the same code path.
func (c *Client) WsTicket(ctx context.Context) (*WSTicket, error) {
	var out WSTicket
	if err := c.do(ctx, http.MethodPost, "/api/auth/ws-ticket", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
