package testsupport_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackathon/tests/e2e/internal/testsupport"
)

// fakeRegisterServer captures the JSON body POSTed to /api/auth/register
// and answers with a minimal success envelope so Register's response
// parsing succeeds.
func fakeRegisterServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(raw, capture); err != nil {
			t.Errorf("decode body: %v body=%s", err, raw)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"token":"tok-1","user":{"id":"USER01","username":"alice"}}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRegisterDefaultBodyShape(t *testing.T) {
	var got map[string]any
	srv := fakeRegisterServer(t, &got)

	userID, token := testsupport.Register(t, srv.URL, "invite-xyz", "alice", "password-1234")

	if userID != "USER01" || token != "tok-1" {
		t.Fatalf("Register returned (%q, %q), want (USER01, tok-1)", userID, token)
	}
	want := map[string]any{
		"username":    "alice",
		"password":    "password-1234",
		"invite_code": "invite-xyz",
	}
	if len(got) != len(want) {
		t.Fatalf("body keys = %v, want %v", keys(got), keys(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, got[k], v)
		}
	}
}

func TestRegisterMergesExtraFields(t *testing.T) {
	var got map[string]any
	srv := fakeRegisterServer(t, &got)

	testsupport.Register(t, srv.URL, "invite-xyz", "alice", "password-1234", testsupport.RegisterOptions{
		ExtraFields: map[string]any{
			"display_name": "Alice",
			"accept_tos":   true,
		},
	})

	want := map[string]any{
		"username":     "alice",
		"password":     "password-1234",
		"invite_code":  "invite-xyz",
		"display_name": "Alice",
		"accept_tos":   true,
	}
	if len(got) != len(want) {
		t.Fatalf("body keys = %v, want %v", keys(got), keys(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%q] = %v (%T), want %v (%T)", k, got[k], got[k], v, v)
		}
	}
}

func TestRegisterExtraFieldsCanOverrideDefaults(t *testing.T) {
	var got map[string]any
	srv := fakeRegisterServer(t, &got)

	testsupport.Register(t, srv.URL, "invite-xyz", "alice", "password-1234", testsupport.RegisterOptions{
		ExtraFields: map[string]any{
			"invite_code": "override-code",
		},
	})

	if got["invite_code"] != "override-code" {
		t.Fatalf("invite_code = %v, want override-code", got["invite_code"])
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
