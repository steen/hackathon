// Package goclient is the typed HTTP + WebSocket client used by apps/cli
// and any other in-tree consumer that needs to talk to the chat server.
//
// All REST methods decode the server envelope ({ok,data,error}) — see
// apps/server/internal/http/errors.go — and return either the typed
// `data` payload or an *APIError carrying the error code/message. WS
// connections are minted via the one-shot ticket flow exposed at
// POST /api/auth/ws-ticket; bearer tokens are never sent on the upgrade.
package goclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Retry/backoff:
//
// This client is fire-once by design. REST calls (auth.go, channels.go,
// messages.go) issue exactly one http.Client.Do per invocation; there
// is no built-in retry, exponential backoff, or jitter, and 429/503
// Retry-After headers are not honored automatically. Network transients
// (connection resets, DNS hiccups, idempotent 5xx) surface to the
// caller as a wrapped error from the underlying transport, and a
// non-2xx envelope surfaces as *APIError.
//
// Retry policy is the caller's responsibility. Pass a context with the
// total deadline you're willing to spend, and wrap calls in whatever
// retry+backoff loop matches the call site's idempotency story —
// retrying PostMessage blindly will duplicate posts. apps/cli/cmd/watch.go
// is the in-tree reference for this pattern (exponential backoff
// capped at 30s for the WS reconnect loop).
//
// WebSocket connections (ws.go) are likewise one-shot at the client
// layer: Watch returns when the stream ends and the caller decides
// whether to mint a fresh ticket and reconnect.
//
// If you find yourself reaching for retries in three places, prefer
// wrapping the *http.Client passed to WithHTTPClient with a retrying
// RoundTripper rather than forking this package — that keeps the
// retry policy under your control and out of the typed API surface.

// DefaultTimeout is the per-request HTTP timeout when the caller does
// not pass a custom transport. WebSocket upgrades do not use it —
// Watch's lifetime is bound to its caller-supplied context.
const DefaultTimeout = 30 * time.Second

// MaxResponseBytes caps the bytes the client will read from a single
// REST response. Mirrors the server's 1 MiB request-body cap so the
// two sides agree on what's worth buffering, and a misbehaving or
// hostile peer cannot make a client buffer arbitrary memory.
const MaxResponseBytes int64 = 1 << 20

// Client is the chat-server client. Construct with New. Token storage
// is in-memory only — persisting it is the caller's job.
type Client struct {
	baseURL string
	http    *http.Client

	mu    sync.RWMutex
	token string
}

// Option mutates a Client during construction.
type Option func(*Client)

// WithHTTPClient replaces the default *http.Client. Useful for tests
// (httptest.Server) or for injecting custom transports/timeouts.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithToken seeds the bearer token at construction. Equivalent to
// calling Token() right after New.
func WithToken(t string) Option {
	return func(c *Client) { c.token = t }
}

// New returns a Client targeting baseURL (e.g. "http://127.0.0.1:8080").
// A trailing slash is tolerated. Options are applied in order.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL returns the configured base URL with no trailing slash.
func (c *Client) BaseURL() string { return c.baseURL }

// Token returns the currently-stored bearer token. Empty when none has
// been set.
func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// SetToken replaces the bearer token. Pass "" to clear. Login and
// Register call this internally on success so a freshly-authenticated
// Client is usable for the rest of the API surface without ceremony.
func (c *Client) SetToken(t string) {
	c.mu.Lock()
	c.token = t
	c.mu.Unlock()
}

// APIError is the typed view of the server's error envelope. Returned
// by every REST method when the server responds with ok=false.
type APIError struct {
	Status  int    // HTTP status code
	Code    string // error.code (e.g. "unauthorized")
	Message string // error.message (user-safe)
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api: %s (%d): %s", e.Code, e.Status, e.Message)
}

// IsCode returns true when err is an *APIError with the given code.
// Convenience for `if errors.Is`-style branching at call sites.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == code
}

// envelope mirrors apps/server/internal/http.Envelope. Data is held
// as RawMessage so each call site can decode into its own concrete
// type without a generic envelope[T].
type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do issues an HTTP request, decodes the envelope, and writes data into
// out (when non-nil). Authorization header is added when a token is set.
// Caller-supplied body, if non-nil, is JSON-encoded and sent with
// Content-Type: application/json.
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := c.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	// A misconfigured upstream proxy can emit 5xx with an empty body;
	// surface a typed error instead of a JSON parse failure so callers
	// can branch on Status without inspecting err strings.
	if len(bytes.TrimSpace(raw)) == 0 {
		if resp.StatusCode >= 400 {
			return &APIError{Status: resp.StatusCode, Code: "unknown", Message: resp.Status}
		}
		return nil
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode envelope (status %d): %w", resp.StatusCode, err)
	}
	if !env.OK {
		apiErr := &APIError{Status: resp.StatusCode}
		if env.Error != nil {
			apiErr.Code = env.Error.Code
			apiErr.Message = env.Error.Message
		}
		return apiErr
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("decode data: %w", err)
		}
	}
	return nil
}
