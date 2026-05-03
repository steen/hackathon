package wsapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackathon/apps/server/internal/hub"
)

type fakeSub struct{ id int }

func (*fakeSub) Send(msg []byte) {}

func TestDebugSubsHandler(t *testing.T) {
	t.Parallel()

	h := hub.New()
	// Distinct ids force distinct addresses — Go can collapse pointers to
	// zero-size structs to a single address, which would dedupe in the hub's
	// map[Subscriber]struct{}.
	h.Subscribe("#general", &fakeSub{id: 1})
	h.Subscribe("#general", &fakeSub{id: 2})
	h.Subscribe("#other", &fakeSub{id: 3})

	tests := []struct {
		name       string
		method     string
		target     string
		remoteAddr string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "general has two subscribers",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "127.0.0.1:54321",
			wantStatus: http.StatusOK,
			wantBody:   "2\n",
		},
		{
			name:       "other has one subscriber",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23other",
			remoteAddr: "127.0.0.1:54322",
			wantStatus: http.StatusOK,
			wantBody:   "1\n",
		},
		{
			name:       "unknown channel reports zero",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23nope",
			remoteAddr: "127.0.0.1:54323",
			wantStatus: http.StatusOK,
			wantBody:   "0\n",
		},
		{
			name:       "ipv6 loopback is allowed",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "[::1]:54324",
			wantStatus: http.StatusOK,
			wantBody:   "2\n",
		},
		{
			name:       "ipv4-mapped ipv6 loopback is allowed",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "[::ffff:127.0.0.1]:54325",
			wantStatus: http.StatusOK,
			wantBody:   "2\n",
		},
		{
			name:       "non-loopback ipv4 is a 404",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "10.0.0.1:54326",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "non-loopback ipv6 is a 404",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "[2001:db8::1]:54327",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing channel parameter is a 400",
			method:     http.MethodGet,
			target:     "/debug/subs",
			remoteAddr: "127.0.0.1:54328",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-GET is a 405",
			method:     http.MethodPost,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "127.0.0.1:54329",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "non-loopback non-GET is still a 404",
			method:     http.MethodPost,
			target:     "/debug/subs?channel=%23general",
			remoteAddr: "10.0.0.1:54330",
			wantStatus: http.StatusNotFound,
		},
	}

	handler := DebugSubsHandler(h)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.RemoteAddr = tc.remoteAddr
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantBody == "" {
				return
			}
			body, err := io.ReadAll(rec.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tc.wantBody {
				t.Fatalf("body: got %q want %q", body, tc.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Fatalf("content-type: got %q, want text/plain prefix", ct)
			}
		})
	}
}
