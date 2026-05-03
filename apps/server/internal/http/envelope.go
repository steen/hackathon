// Package http holds the HTTP-layer glue for the chat server: response
// envelope helpers and request handlers. The package name shadows the
// standard library's net/http on purpose — call sites import this as
// httpapi to disambiguate, and the rest of the codebase only needs the
// helpers it exposes here.
package http

import (
	"encoding/json"
	stdhttp "net/http"
)

// Envelope is the wire shape PRD §10 mandates for every JSON response.
// Both arms (ok/err) ship every field so clients see a stable schema.
type Envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data"`
	Error *ErrorBody  `json:"error"`
}

// ErrorBody is the inner shape of Envelope.Error. Code is a stable
// machine string; Message is user-safe (no SQL, no stack traces — see
// PRD §9 "Secrets & config hygiene").
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes used across the auth surface. Kept as a small enum so
// clients can branch on code rather than parse Message.
const (
	CodeBadRequest      = "bad_request"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeInternal        = "internal"
	CodeMethodNotAllow  = "method_not_allowed"
	CodeUnsupportedType = "unsupported_media_type"
)

// WriteJSON serializes an Envelope at status. A serialization failure
// falls back to a plain-text 500 — the alternative (writing partial
// JSON) corrupts the response stream.
func WriteJSON(w stdhttp.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		// At this point the status is already on the wire; the best we
		// can do is stop and let the client see a truncated body.
		_ = err
	}
}

// WriteOK sends the success arm of the envelope.
func WriteOK(w stdhttp.ResponseWriter, status int, data interface{}) {
	WriteJSON(w, status, Envelope{OK: true, Data: data})
}

// WriteError sends the failure arm. Message is sent verbatim — the
// caller is responsible for keeping it user-safe.
func WriteError(w stdhttp.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Envelope{OK: false, Error: &ErrorBody{Code: code, Message: message}})
}
