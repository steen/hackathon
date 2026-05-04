package auth_endpoint_paths_align_with_prd_e2e_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

// TestAC1_HandlersRegisteredAtPRDPaths covers AC-1 verbatim:
//
//	"The server registers handlers at /api/auth/register,
//	/api/auth/login, /api/auth/me, /api/auth/logout, /api/auth/ws-ticket
//	(matching PRD §10)."
//
// It boots the production chat-server binary and probes each of the
// five paths with an unauthenticated request. A registered handler
// produces an application-level rejection (e.g. 400/401/405); only an
// unmounted route yields the default ServeMux 404. Asserting non-404
// proves each path is wired up. The test also probes the pre-rename
// `/api/<verb>` paths and asserts they DO 404 — proving the server
// has not silently aliased both shapes.
func TestAC1_HandlersRegisteredAtPRDPaths(t *testing.T) {
	t.Parallel()

	srv := startServer(t)
	client := &http.Client{Timeout: 10 * time.Second}

	prdPaths := []string{
		"/api/auth/register",
		"/api/auth/login",
		"/api/auth/me",
		"/api/auth/logout",
		"/api/auth/ws-ticket",
	}
	for _, p := range prdPaths {
		t.Run("mounted_"+p, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.httpURL+p, bytes.NewReader([]byte("{}")))
			if err != nil {
				t.Fatalf("new request %s: %v", p, err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("http.Do %s: %v", p, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf("%s returned 404; AC-1 requires a handler mounted at this path", p)
			}
		})
	}

	oldPaths := []string{
		"/api/register",
		"/api/login",
		"/api/me",
		"/api/logout",
		"/api/ws-ticket",
	}
	for _, p := range oldPaths {
		t.Run("unmounted_"+p, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.httpURL+p, bytes.NewReader([]byte("{}")))
			if err != nil {
				t.Fatalf("new request %s: %v", p, err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("http.Do %s: %v", p, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s returned %d; pre-rename path must 404 to prove no silent alias", p, resp.StatusCode)
			}
		})
	}
}
