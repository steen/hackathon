package http

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"time"
)

// redactedQueryKeys are query parameters that must never appear in access
// logs. Per PRD §9 SEC-11 the access log of a login + WS upgrade contains
// no token or ticket value. The session JWT is also forbidden as a query
// parameter on any path; ticket is the one-shot 30s WS auth handle.
var redactedQueryKeys = []string{"token", "ticket"}

const redactedValue = "REDACTED"

// statusRecorder captures the response status so the access log can record
// it after the handler returns. It defaults to 200 because handlers that
// return without calling WriteHeader implicitly send 200.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// WriteHeader captures the response status before forwarding to the wrapped
// ResponseWriter. Repeated calls are idempotent.
func (s *statusRecorder) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.status = code
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// Hijack forwards to the underlying ResponseWriter's Hijacker so this
// middleware can wrap a handler that upgrades the connection (e.g. /ws).
// Without this method, wrapping silently drops the Hijacker interface and
// websocket.Accept fails.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	// Once hijacked, status is meaningless; the upgrade has taken over.
	s.wrote = true
	return h.Hijack()
}

// Flush forwards to the underlying ResponseWriter's Flusher so streaming
// handlers (SSE, etc.) keep working through this middleware.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// AccessLog wraps next and emits one structured-ish log line per request.
// Sensitive query parameters (see redactedQueryKeys) are stripped before
// the URL is logged. Latency is wall clock; status is observed via the
// wrapped ResponseWriter.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		redacted := redactURL(r.URL)
		log.Printf("access method=%s path=%s status=%d latency_ms=%d request_id=%s",
			r.Method,
			redacted,
			rec.status,
			time.Since(start).Milliseconds(),
			RequestID(r.Context()),
		)
	})
}

// redactURL returns the request-target form (path + query, no scheme/host)
// with sensitive query keys replaced by REDACTED. Replacement preserves the
// key's presence so an operator can see "ticket was passed" without seeing
// the value. Parsing is via net/url so byte-level oddities (encoding,
// ordering, repeated keys) cannot smuggle a raw token into the log.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.EscapedPath()
	}
	q := u.Query()
	changed := false
	for _, k := range redactedQueryKeys {
		if _, ok := q[k]; ok {
			q.Set(k, redactedValue)
			changed = true
		}
	}
	if !changed {
		return u.EscapedPath() + "?" + u.RawQuery
	}
	return u.EscapedPath() + "?" + q.Encode()
}

// Recover catches panics from downstream handlers, logs the panic value and
// stack with the request ID, and writes a generic 500 envelope. The client
// never sees the panic value or stack frames (PRD §11 / SEC envelope
// hygiene).
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rv := recover()
			if rv == nil {
				return
			}
			log.Printf("panic request_id=%s value=%v\n%s",
				RequestID(r.Context()), rv, debug.Stack())
			WriteError(w, http.StatusInternalServerError, CodeInternal, "internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestIDMiddleware assigns a fresh request ID to every request and
// stores it on the context so downstream handlers and the access log can
// reach it via RequestID(ctx). The ID is also echoed in the
// X-Request-Id response header so an operator can correlate a client
// report with a server log line.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		ctx := WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure on a healthy host is essentially impossible;
		// fall back to a timestamp-derived ID rather than panic.
		return "req-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
