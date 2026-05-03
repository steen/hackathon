package http

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// RESTBodyLimit is the per-request REST body cap (PRD §9, SEC-7).
const RESTBodyLimit int64 = 16 * 1024

// MessageBodyLimit is the cap on chat-message bodies (PRD §9, SEC-8).
// Enforced both on REST POST and on decoded WS frames.
const MessageBodyLimit = 4 * 1024

// BodyCap wraps next so every request body is limited to RESTBodyLimit.
// On overflow the middleware writes a 413 envelope itself rather than
// letting the downstream handler discover the failure mid-decode — this
// keeps the response shape uniform regardless of the handler.
//
// The middleware reads the body eagerly up to the limit. Handlers see
// an unread *bytes.Reader (Content-Length is preserved) and can decode
// as usual.
func BodyCap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		limited := http.MaxBytesReader(w, r.Body, RESTBodyLimit)
		buf, err := io.ReadAll(limited)
		if err != nil {
			if IsBodyTooLarge(err) {
				WriteBodyTooLarge(w)
				return
			}
			WriteError(w, http.StatusBadRequest, CodeBadRequest, "could not read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(buf))
		r.ContentLength = int64(len(buf))
		next.ServeHTTP(w, r)
	})
}

// IsBodyTooLarge reports whether err originated from MaxBytesReader hitting
// the limit.
func IsBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// WriteBodyTooLarge writes the canonical 413 envelope.
func WriteBodyTooLarge(w http.ResponseWriter) {
	WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 16 KiB limit")
}

// WriteMessageTooLarge writes the canonical 400 envelope for an
// oversize chat-message body (SEC-8, REST path).
func WriteMessageTooLarge(w http.ResponseWriter) {
	WriteError(w, http.StatusBadRequest, "message_too_large", "message body exceeds 4 KiB limit")
}
