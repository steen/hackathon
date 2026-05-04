// AC-3: Headers are present on both 2xx and error responses.
//
// Source: specs/plans/phase-1/feature-file-perms-and-headers.md.
//
// AC-2 (security_headers_test.go) already pins the four SEC-10 headers on
// 200/401/404. AC-3 layers failure-mode coverage on top: the contract is
// that the SecurityHeaders middleware is the outermost wrap, so even when
// inner middleware (BodyCap, ServeMux's MethodNotAllowed branch, or a
// handler returning a 4xx envelope) writes the response, the outer wrap
// has already populated the headers.
//
// The error-class matrix below picks one route per status class so we hit
// distinct write paths inside the server:
//
//   - 200 — GET /debug/subs?channel=%23general (loopback-only debug route).
//   - 400 — POST /api/auth/login with a non-JSON body (decodeJSON fails).
//   - 401 — GET /api/auth/me with no Authorization (auth middleware).
//   - 404 — GET /nope (default ServeMux fallback).
//   - 405 — GET /api/auth/login (login is POST-only; handler writes 405).
//   - 413 — POST /api/auth/register with a 16385-byte body (BodyCap fires
//     before the handler runs, proving SecurityHeaders wraps BodyCap).
//
// 500 is intentionally omitted: there is no panic-probe build tag wired
// into apps/server today, so a 500 cannot be triggered from outside the
// binary without modifying production code. The findings sketch in
// specs/test-analysis/phase-1/file-perms-and-headers.md called this out
// as conditional on the panic probe; we flag the gap in the report
// rather than skipping a test that cannot be run.
package file_perms_and_headers_e2e_test

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestAC3_SecurityHeadersPresentOnBothSuccessAndErrorResponses — verbatim:
// "Headers are present on both 2xx and error responses." Per
// specs/plans/phase-1/feature-file-perms-and-headers.md.
func TestAC3_SecurityHeadersPresentOnBothSuccessAndErrorResponses(t *testing.T) {
	srv := startServer(t, startServerOpts{})

	client := &http.Client{Timeout: 5 * time.Second}

	// oversize is one byte past the 16 KiB REST body cap enforced by
	// apps/server/internal/http/limits.go (RESTBodyLimit = 16*1024). A
	// payload of exactly 16385 bytes fails MaxBytesReader and triggers
	// WriteBodyTooLarge → 413 from inside BodyCap.
	const oversizeLen = 16*1024 + 1
	oversize := bytes.Repeat([]byte("a"), oversizeLen)

	cases := []struct {
		name       string
		method     string
		path       string
		body       []byte
		wantStatus int
	}{
		{
			name:       "success_2xx_debug_subs",
			method:     http.MethodGet,
			path:       "/debug/subs?channel=" + url.QueryEscape("#general"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "error_400_bad_json_login",
			method:     http.MethodPost,
			path:       "/api/auth/login",
			body:       []byte("garbage-not-json"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "error_401_unauth_me",
			method:     http.MethodGet,
			path:       "/api/auth/me",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "error_404_unknown_route",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "error_405_get_login",
			method:     http.MethodGet,
			path:       "/api/auth/login",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "error_413_oversize_register",
			method:     http.MethodPost,
			path:       "/api/auth/register",
			body:       oversize,
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var body *bytes.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			}
			var req *http.Request
			var err error
			if body != nil {
				req, err = http.NewRequestWithContext(ctx, tc.method, srv.httpURL+tc.path, body)
			} else {
				req, err = http.NewRequestWithContext(ctx, tc.method, srv.httpURL+tc.path, nil)
			}
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if tc.body != nil && strings.Contains(tc.path, "/api/") {
				req.Header.Set("Content-Type", "application/json")
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
