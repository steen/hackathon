package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// SEC-10: every response carries the four security headers verbatim, on both
// 2xx and error paths.

func wantHeaders() map[string]string {
	return map[string]string{
		"Content-Security-Policy": CSP,
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
	}
}

func assertHeaders(t *testing.T, got http.Header) {
	t.Helper()
	for k, want := range wantHeaders() {
		if g := got.Get(k); g != want {
			t.Errorf("header %q: got %q, want %q", k, g, want)
		}
	}
}

func TestSecurityHeaders_CSPLiteralMatchesPRD_SEC10(t *testing.T) {
	const prd = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
	if CSP != prd {
		t.Fatalf("CSP drifted from PRD §9.\n got:  %q\n want: %q", CSP, prd)
	}
}

func TestSecurityHeaders_OK_SEC10(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	assertHeaders(t, rec.Result().Header)
}

func TestSecurityHeaders_ErrorResponse_SEC10(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	assertHeaders(t, rec.Result().Header)
}

func TestSecurityHeaders_NotFound_SEC10(t *testing.T) {
	mux := http.NewServeMux()
	h := SecurityHeaders(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	assertHeaders(t, rec.Result().Header)
}

func TestSecurityHeaders_PassthroughBody(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := rec.Body.String(); got != "hello" {
		t.Fatalf("body: got %q, want %q", got, "hello")
	}
}
