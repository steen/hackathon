// Package httpx contains HTTP-side helpers shared across handlers:
// the response envelope writer and the body-cap middleware.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Envelope is the standard response shape: { ok, data, error }.
// One of data / error is non-nil; both nil means a bare ok response.
type Envelope struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data"`
	Error *ErrorBody  `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes a non-ok envelope with the given user-safe code and
// message and the given HTTP status. Never include SQL errors, stack
// traces, or driver messages here — those go to server logs only.
func WriteError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		OK:    false,
		Data:  nil,
		Error: &ErrorBody{Code: code, Message: message},
	})
}
