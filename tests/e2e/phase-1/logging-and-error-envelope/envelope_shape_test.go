package logging_and_error_envelope_e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAC3_EnvelopeShapeIsConsistent covers AC-3 of
// specs/plans/phase-1/feature-logging-and-error-envelope.md verbatim:
//
//	Every JSON response uses the envelope { ok: bool, data: any|null,
//	error: { code: string, message: string }|null } per PRD §6. ok=false
//	implies error is non-null and data is null; ok=true implies the
//	inverse.
//
// The test boots the server and probes a representative cross-section of
// the JSON HTTP surface — both success arms and failure arms across the
// auth and channels handlers — then asserts the envelope invariant on
// every JSON response. Non-JSON responses (text/plain 404 from the
// default mux, the WS upgrade) are excluded by Content-Type, matching
// the AC's "every JSON response" wording.
//
// The invariant being asserted (per case):
//
//   - all three top-level keys are present (so clients can rely on the
//     three-key shape — `data: null` and `error: null` ship explicitly,
//     never as missing keys);
//   - ok is a JSON bool;
//   - if ok=true: data is non-null AND error is JSON null;
//   - if ok=false: data is JSON null AND error is non-null with
//     non-empty `code` and `message` strings.
//
// Each probe carries a minimal expected status so a regression that
// changed e.g. duplicate-name from 409 to 200 surfaces in this test
// rather than masking the envelope assertion.
func TestAC3_EnvelopeShapeIsConsistent(t *testing.T) {
	srv := startServer(t)

	_, tok := register(t, srv, "alice-ac3", "correct-horse-battery")

	// existingChannel name is reused below to trigger a 409 conflict in
	// the failure probes.
	const existingChannel = "ac3-room"
	createStatus, _, createEnv, createRaw := postJSON(t, srv, "/api/channels", tok, map[string]string{
		"name": existingChannel,
	})
	if createStatus != http.StatusCreated && createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/channels: status %d body %s", createStatus, createRaw)
	}
	if !createEnv.OK {
		t.Fatalf("seed POST /api/channels: ok=false body %s", createRaw)
	}

	type probe struct {
		name       string
		method     string
		path       string
		bearer     string
		body       []byte // raw bytes; nil → no body
		ctype      string // Content-Type for the request; empty → omit
		wantStatus int
		wantOK     bool
	}

	registerBody := mustJSON(t, map[string]string{
		"username":    "bob-ac3",
		"password":    "correct-horse-battery",
		"invite_code": srv.inviteCode,
	})
	loginGoodBody := mustJSON(t, map[string]string{
		"username": "alice-ac3",
		"password": "correct-horse-battery",
	})
	loginBadBody := mustJSON(t, map[string]string{
		"username": "alice-ac3",
		"password": "wrong-password",
	})
	createDupBody := mustJSON(t, map[string]string{"name": existingChannel})
	createBadShapeBody := mustJSON(t, map[string]string{"name": ""})

	probes := []probe{
		// ---- ok=true ----
		{
			name:       "register-201",
			method:     http.MethodPost,
			path:       "/api/auth/register",
			body:       registerBody,
			ctype:      "application/json",
			wantStatus: http.StatusCreated,
			wantOK:     true,
		},
		{
			name:       "login-200",
			method:     http.MethodPost,
			path:       "/api/auth/login",
			body:       loginGoodBody,
			ctype:      "application/json",
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "me-200",
			method:     http.MethodGet,
			path:       "/api/auth/me",
			bearer:     tok,
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "channels-list-200",
			method:     http.MethodGet,
			path:       "/api/channels",
			bearer:     tok,
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		// ---- ok=false ----
		{
			name:       "login-wrong-password-401",
			method:     http.MethodPost,
			path:       "/api/auth/login",
			body:       loginBadBody,
			ctype:      "application/json",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
		{
			name:       "me-no-auth-401",
			method:     http.MethodGet,
			path:       "/api/auth/me",
			wantStatus: http.StatusUnauthorized,
			wantOK:     false,
		},
		{
			name:       "register-malformed-json-400",
			method:     http.MethodPost,
			path:       "/api/auth/register",
			body:       []byte("{not valid json"),
			ctype:      "application/json",
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
		{
			name:       "login-malformed-json-400",
			method:     http.MethodPost,
			path:       "/api/auth/login",
			body:       []byte("][}"),
			ctype:      "application/json",
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
		{
			name:       "channels-create-duplicate-409",
			method:     http.MethodPost,
			path:       "/api/channels",
			bearer:     tok,
			body:       createDupBody,
			ctype:      "application/json",
			wantStatus: http.StatusConflict,
			wantOK:     false,
		},
		{
			name:       "channels-create-bad-shape-400",
			method:     http.MethodPost,
			path:       "/api/channels",
			bearer:     tok,
			body:       createBadShapeBody,
			ctype:      "application/json",
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
		{
			name:       "messages-list-bad-channel-400",
			method:     http.MethodGet,
			path:       "/api/channels/not-a-ulid/messages",
			bearer:     tok,
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
		{
			name:       "method-not-allowed-405",
			method:     http.MethodDelete,
			path:       "/api/auth/register",
			wantStatus: http.StatusMethodNotAllowed,
			wantOK:     false,
		},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			status, hdr, raw := rawRequest(t, srv, p.method, p.path, p.bearer, p.ctype, p.body)
			if status != p.wantStatus {
				t.Fatalf("%s %s: status=%d, want %d (body=%s)", p.method, p.path, status, p.wantStatus, raw)
			}
			if !isJSON(hdr.Get("Content-Type")) {
				t.Fatalf("%s %s: Content-Type=%q, want application/json (body=%s)",
					p.method, p.path, hdr.Get("Content-Type"), raw)
			}
			requireEnvelope(t, raw, p.wantOK)
		})
	}
}

// requireEnvelope decodes raw with all three top-level fields as
// json.RawMessage so the assertion can distinguish "key is JSON null"
// from "key is missing" — the AC requires both arms ship the full
// three-key shape with explicit nulls. wantOK toggles the invariant
// arm (success vs failure).
func requireEnvelope(t *testing.T, raw []byte, wantOK bool) {
	t.Helper()

	// Decode into a json.RawMessage map so the manual key-set loop below
	// (the `for k := range top` block) can flag any unexpected top-level
	// key as a contract violation while still distinguishing "missing
	// key" from "unknown key" with separate error messages.
	var top map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&top); err != nil {
		t.Fatalf("envelope: cannot decode JSON object: %v\nbody=%s", err, raw)
	}

	for _, k := range []string{"ok", "data", "error"} {
		if _, present := top[k]; !present {
			t.Fatalf("envelope: missing top-level key %q\nbody=%s", k, raw)
		}
	}
	for k := range top {
		switch k {
		case "ok", "data", "error":
		default:
			t.Errorf("envelope: unexpected top-level key %q\nbody=%s", k, raw)
		}
	}

	var ok bool
	if err := json.Unmarshal(top["ok"], &ok); err != nil {
		t.Fatalf("envelope: ok is not a bool: %v\nbody=%s", err, raw)
	}
	if ok != wantOK {
		t.Fatalf("envelope: ok=%v, want %v\nbody=%s", ok, wantOK, raw)
	}

	dataNull := isJSONNull(top["data"])
	errorNull := isJSONNull(top["error"])

	if ok {
		// ok=true ⇒ error is null AND data is non-null.
		if !errorNull {
			t.Errorf("envelope: ok=true but error is non-null (%s)\nbody=%s", top["error"], raw)
		}
		if dataNull {
			t.Errorf("envelope: ok=true but data is null\nbody=%s", raw)
		}
		return
	}

	// ok=false ⇒ data is null AND error is non-null with non-empty
	// code+message strings.
	if !dataNull {
		t.Errorf("envelope: ok=false but data is non-null (%s)\nbody=%s", top["data"], raw)
	}
	if errorNull {
		t.Fatalf("envelope: ok=false but error is null\nbody=%s", raw)
	}
	var errBody struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(top["error"], &errBody); err != nil {
		t.Fatalf("envelope: error not decodable as {code,message}: %v\nbody=%s", err, raw)
	}
	if errBody.Code == "" {
		t.Errorf("envelope: error.code is empty\nbody=%s", raw)
	}
	if errBody.Message == "" {
		t.Errorf("envelope: error.message is empty\nbody=%s", raw)
	}
}

// rawRequest issues a single HTTP request without going through the
// envelope decode path — AC-3 needs to inspect the raw bytes (including
// the Content-Type header) and tolerate non-JSON bodies en route to
// failing the test.
func rawRequest(t *testing.T, srv *runningServer, method, path, bearer, ctype string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.httpURL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s %s: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header, raw
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %v: %v", v, err)
	}
	return b
}

// isJSONNull treats a missing/empty raw message as null too — both
// shapes signal "no value present" to the caller, which is the same
// signal `data: null` ships on the wire.
func isJSONNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}

func isJSON(ct string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json")
}
