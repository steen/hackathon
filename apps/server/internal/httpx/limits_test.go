package httpx_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackathon/apps/server/internal/httpx"
)

// TestRESTRejectsBodyOver16KiBWith413 — covers PRD §11 SEC-7.
func TestRESTRejectsBodyOver16KiBWith413(t *testing.T) {
	called := false
	h := httpx.BodyCap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("a"), int(httpx.RESTBodyLimit)+1)
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Fatalf("downstream handler should not run when body cap is exceeded")
	}

	var env httpx.Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%q", err, rr.Body.String())
	}
	if env.OK {
		t.Fatalf("envelope ok=true; want false")
	}
	if env.Error == nil {
		t.Fatalf("envelope error nil; want non-nil")
	}
	if env.Error.Code != "body_too_large" {
		t.Fatalf("error code: got %q want %q", env.Error.Code, "body_too_large")
	}
	if env.Error.Message == "" {
		t.Fatalf("error message empty")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type: got %q want application/json...", ct)
	}
}

func TestRESTAllowsBodyAtLimit(t *testing.T) {
	called := false
	var seenLen int
	h := httpx.BodyCap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		seenLen = len(buf)
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.Repeat([]byte("a"), int(httpx.RESTBodyLimit))
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("handler not called for at-limit body")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if seenLen != int(httpx.RESTBodyLimit) {
		t.Fatalf("downstream read len: got %d want %d", seenLen, httpx.RESTBodyLimit)
	}
}

// TestWriteMessageTooLargeEnvelope — covers PRD §11 SEC-8 (REST path
// envelope shape; the WS path is asserted in wsapi tests).
func TestWriteMessageTooLargeEnvelope(t *testing.T) {
	rr := httptest.NewRecorder()
	httpx.WriteMessageTooLarge(rr)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
	var env httpx.Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "message_too_large" {
		t.Fatalf("envelope: %+v", env)
	}
}
