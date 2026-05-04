// AC-2: HTTP responses include the following headers on all routes:
//   - Content-Security-Policy — restrictive policy suitable for the embedded
//     web app.
//   - X-Content-Type-Options: nosniff
//   - Referrer-Policy: no-referrer
//   - X-Frame-Options: DENY
//
// Source: specs/plans/phase-1/feature-file-perms-and-headers.md.
package file_perms_and_headers_e2e_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// expectedCSP mirrors the value pinned in
// apps/server/internal/http/headers_middleware.go (const CSP). Hard-coded
// here on purpose: the test fails loudly if either side drifts. AC-2 only
// requires the header to be present and "restrictive"; pinning the exact
// string also catches accidental relaxations during refactors.
const expectedCSP = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// requireSecHeaders asserts the four SEC-10 headers from AC-2 are present
// with the documented values. label identifies the call site in failures.
func requireSecHeaders(t *testing.T, label string, h http.Header) {
	t.Helper()
	if got := h.Get("Content-Security-Policy"); got != expectedCSP {
		t.Errorf("%s: Content-Security-Policy = %q; want %q", label, got, expectedCSP)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options = %q; want %q", label, got, "nosniff")
	}
	if got := h.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("%s: Referrer-Policy = %q; want %q", label, got, "no-referrer")
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("%s: X-Frame-Options = %q; want %q", label, got, "DENY")
	}
}

// TestAC2_ResponseHeadersIncludeSecurityHeaders — "HTTP responses include
// the following headers on all routes: Content-Security-Policy,
// X-Content-Type-Options: nosniff, Referrer-Policy: no-referrer,
// X-Frame-Options: DENY." Verbatim from
// specs/plans/phase-1/feature-file-perms-and-headers.md.
//
// Hits a representative set of routes covering different status classes so
// we exercise more than one mux branch:
//
//   - GET /debug/subs?channel=%23general — 200 from loopback.
//   - GET /api/auth/me with no Authorization — 401 from the auth-required
//     middleware.
//   - GET /nope — 404 from the default ServeMux fallback.
//
// AC-3 ("headers are present on both 2xx and error responses") layers
// failure-mode coverage on top of this; that is a separate test.
func TestAC2_ResponseHeadersIncludeSecurityHeaders(t *testing.T) {
	srv := startServer(t, startServerOpts{})

	client := &http.Client{Timeout: 5 * time.Second}

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "debug_subs_200",
			method:     http.MethodGet,
			path:       "/debug/subs?channel=" + url.QueryEscape("#general"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "auth_me_unauth_401",
			method:     http.MethodGet,
			path:       "/api/auth/me",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown_route_404",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, tc.method, srv.httpURL+tc.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do %s %s: %v", tc.method, tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d; want %d", resp.StatusCode, tc.wantStatus)
			}
			requireSecHeaders(t, tc.name, resp.Header)
		})
	}
}
