// Package http holds HTTP middleware and shared response helpers used by
// every server handler. The envelope is the wire contract from PRD §10:
// every JSON response carries the three keys ok, data, error.
package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

// Envelope is the {ok,data,error} response shape from PRD §10.
type Envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data"`
	Error *ErrorBody  `json:"error"`
}

// ErrorBody is the user-safe error payload nested in Envelope.Error.
// Code is a stable machine string clients branch on; Message is
// user-safe text (no SQL, no stack traces, no file paths — see PRD §9
// "Secrets & config hygiene").
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes used across the HTTP surface. Kept as a small enum so
// clients can branch on code rather than parse Message.
const (
	CodeBadRequest      = "bad_request"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeConflict        = "conflict"
	CodeInternal        = "internal"
	CodeMethodNotAllow  = "method_not_allowed"
	CodeUnsupportedType = "unsupported_media_type"
)

// WriteJSON serializes env at status. A serialization failure logs and
// stops; the headers are already on the wire so a partial body is
// preferable to a panic.
func WriteJSON(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		log.Printf("envelope encode: %v", err)
	}
}

// WriteOK sends the success arm of the envelope at status. data may be
// nil — the envelope still ships data: null, ok: true, error: null so
// clients can rely on all three keys being present.
func WriteOK(w http.ResponseWriter, status int, data interface{}) {
	WriteJSON(w, status, Envelope{OK: true, Data: data, Error: nil})
}

// WriteError sends the failure arm of the envelope at status. message
// must be safe to surface to a remote client — no SQL text, stack
// frames, or file paths. The caller is responsible for logging the
// underlying detail server-side.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Envelope{
		OK:    false,
		Data:  nil,
		Error: &ErrorBody{Code: code, Message: message},
	})
}

type ctxKey int

const requestIDKey ctxKey = iota

// WithRequestID returns a child context carrying id. Used by middleware to
// plumb the per-request ID into handlers.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the per-request ID set by RequestIDMiddleware. Returns
// an empty string when no ID is present (handler invoked without the
// middleware, e.g. in tests).
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}
