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
		wantStatus int
		wantBody   string
	}{
		{
			name:       "general has two subscribers",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23general",
			wantStatus: http.StatusOK,
			wantBody:   "2\n",
		},
		{
			name:       "other has one subscriber",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23other",
			wantStatus: http.StatusOK,
			wantBody:   "1\n",
		},
		{
			name:       "unknown channel reports zero",
			method:     http.MethodGet,
			target:     "/debug/subs?channel=%23nope",
			wantStatus: http.StatusOK,
			wantBody:   "0\n",
		},
		{
			name:       "missing channel parameter is a 400",
			method:     http.MethodGet,
			target:     "/debug/subs",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-GET is a 405",
			method:     http.MethodPost,
			target:     "/debug/subs?channel=%23general",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	handler := DebugSubsHandler(h)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
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
