package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hackathon/apps/server/internal/auth"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/ratelimit"
	"hackathon/migrations"
)

// rlFixture wires a fresh DB + handlers + the IP rate-limit middleware
// in front of /api/auth/login and /api/auth/register, mirroring main.go.
type rlFixture struct {
	mux      *stdhttp.ServeMux
	handlers *AuthHandlers
	closeFn  func()
}

func newRateLimitFixture(t *testing.T, loginCfg, registerCfg ratelimit.IPLimiterConfig, userCfg *ratelimit.UserLimiterConfig) *rlFixture {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := appdb.Open(dir + "/rl.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := appdb.ApplyFS(t.Context(), sqlDB, migrations.FS); err != nil {
		t.Fatalf("ApplyFS: %v", err)
	}
	tickets := auth.NewTicketStore()
	var ul *ratelimit.UserLimiter
	if userCfg != nil {
		ul = ratelimit.NewUserLimiter(*userCfg)
	}
	h := NewAuthHandlers(AuthDeps{
		DB:          sqlDB,
		Tickets:     tickets,
		SigningKey:  []byte("test-signing-key-must-be-long-enough"),
		InviteCode:  "INVITE-OK",
		Now:         time.Now,
		UserLimiter: ul,
	})
	loginRL := IPRateLimit(ratelimit.NewIPLimiter(loginCfg), 5*time.Minute, h.AuditSink(), false)
	registerRL := IPRateLimit(ratelimit.NewIPLimiter(registerCfg), 15*time.Minute, h.AuditSink(), false)
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/auth/register", registerRL(stdhttp.HandlerFunc(h.Register)))
	mux.Handle("/api/auth/login", loginRL(stdhttp.HandlerFunc(h.Login)))
	return &rlFixture{
		mux:      mux,
		handlers: h,
		closeFn:  func() { _ = sqlDB.Close() },
	}
}

func (f *rlFixture) close() { f.closeFn() }

func (f *rlFixture) post(t *testing.T, path string, body interface{}, ip string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(stdhttp.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.RemoteAddr = ip + ":12345"
	}
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	return rr
}

// SEC-5 — the 11th login attempt within 5 min from one IP returns 429.
// PRD §11. Test name and assertion both call out the exact threshold
// per the planning correction (PR #20).
func TestIPRateLimitBlocksEleventhLoginAttemptWithin5min(t *testing.T) {
	f := newRateLimitFixture(t, ratelimit.LoginIPConfig(), ratelimit.RegisterIPConfig(), nil)
	defer f.close()

	body := map[string]string{"username": "nobody", "password": "anything-at-all"}
	for i := 1; i <= 10; i++ {
		rr := f.post(t, "/api/auth/login", body, "9.9.9.9")
		if rr.Code == stdhttp.StatusTooManyRequests {
			t.Fatalf("attempt %d/10 already 429; SEC-5 requires the 11th to be the first 429", i)
		}
	}
	rr := f.post(t, "/api/auth/login", body, "9.9.9.9")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("11th login attempt within 5 min from one IP must return 429 (SEC-5); got %d", rr.Code)
	}
}

func TestIPRateLimitBlocksRegisterAfterBurst(t *testing.T) {
	f := newRateLimitFixture(t, ratelimit.LoginIPConfig(), ratelimit.RegisterIPConfig(), nil)
	defer f.close()

	for i := 1; i <= 5; i++ {
		rr := f.post(t, "/api/auth/register", map[string]string{
			"username":    "user-x",
			"password":    "correct-horse-battery",
			"invite_code": "WRONG", // 403 doesn't refund the bucket
		}, "8.8.8.8")
		if rr.Code == stdhttp.StatusTooManyRequests {
			t.Fatalf("attempt %d should fit within burst=5", i)
		}
	}
	rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    "user-x",
		"password":    "correct-horse-battery",
		"invite_code": "WRONG",
	}, "8.8.8.8")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("6th register attempt should 429; got %d", rr.Code)
	}
}

func TestRateLimitedResponseUsesEnvelope(t *testing.T) {
	f := newRateLimitFixture(t, ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour}, ratelimit.RegisterIPConfig(), nil)
	defer f.close()

	body := map[string]string{"username": "x", "password": "y"}
	if rr := f.post(t, "/api/auth/login", body, "7.7.7.7"); rr.Code == stdhttp.StatusTooManyRequests {
		t.Fatal("first attempt should not be limited")
	}
	rr := f.post(t, "/api/auth/login", body, "7.7.7.7")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("second attempt status: got %d want 429", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Errorf("Retry-After header missing on 429")
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: got %q want application/json", ct)
	}
	var env envelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK {
		t.Error("ok=true on 429")
	}
	if env.Error == nil || env.Error.Code != CodeRateLimited {
		t.Errorf("error envelope: %+v", env.Error)
	}
}

func TestSuccessfulLoginResetsUsernameBackoff(t *testing.T) {
	// A virtual clock keeps the gate window observable regardless of
	// real-time scheduler jitter — the previous wall-clock version flaked
	// on loaded CI runners when bcrypt verify time between two posts
	// exceeded the 50ms gate, expiring it before the final assertion.
	now := time.Unix(0, 0)
	uc := ratelimit.LoginUserConfig()
	uc.GraceFailures = 0
	uc.Step = 50 * time.Millisecond
	uc.MaxDelay = 100 * time.Millisecond
	uc.Now = func() time.Time { return now }
	f := newRateLimitFixture(t, ratelimit.LoginIPConfig(), ratelimit.RegisterIPConfig(), &uc)
	defer f.close()

	if rr := f.post(t, "/api/auth/register", map[string]string{
		"username":    "alice",
		"password":    "correct-horse-battery",
		"invite_code": "INVITE-OK",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("register: %d", rr.Code)
	}

	// One bad password registers a failure on the user limiter.
	if rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "wrong-password-xyz",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("bad login status: %d", rr.Code)
	}
	// Step past the gate, then succeed → should reset the counter.
	now = now.Add(120 * time.Millisecond)
	if rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "correct-horse-battery",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusOK {
		t.Fatalf("good login status: %d", rr.Code)
	}
	// One subsequent failure should land within the grace (here =0)
	// window with a fresh failure count of 1, so the Allow() check on
	// the *next* attempt is what gates. Verify by a wrong password,
	// then immediately retry — if Reset didn't fire, this would gate
	// instantly with a multi-step delay; instead it is a single Step.
	if rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "wrong-again-here",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("post-reset bad login: %d", rr.Code)
	}
	rr := f.post(t, "/api/auth/login", map[string]string{
		"username": "alice", "password": "wrong-again-here",
	}, "1.1.1.1")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("expected 429 from username gate; got %d", rr.Code)
	}
	ra := rr.Header().Get("Retry-After")
	// One step (50ms) rounds up to 1s in Retry-After.
	if ra != "1" {
		t.Errorf("Retry-After: got %q want 1 (one Step worth, rounded up)", ra)
	}
}

func TestRateLimitRejectionLoggedToAuthEvents(t *testing.T) {
	f := newRateLimitFixture(t, ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour}, ratelimit.RegisterIPConfig(), nil)
	defer f.close()

	body := map[string]string{"username": "x", "password": "y"}
	_ = f.post(t, "/api/auth/login", body, "5.5.5.5")
	rr := f.post(t, "/api/auth/login", body, "5.5.5.5")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("setup: expected 429 on second call; got %d", rr.Code)
	}

	var n int
	row := f.handlers.deps.DB.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind = ?`, AuthEventRateLimited)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("query auth_events: %v", err)
	}
	if n < 1 {
		t.Fatalf("auth_events rate_limited rows: got %d want ≥1", n)
	}
}

// PRD §9 / §11: with trustedProxy=true, the per-IP rate-limit bucket
// keys on the leftmost X-Forwarded-For entry — without this the bucket
// collapses to a single key behind a reverse proxy. We mount a Burst=2
// limiter behind one constant proxy RemoteAddr but vary the XFF
// leftmost across two values, and assert each key gets its own bucket.
func TestIPRateLimitKeysOnXFFWhenTrustedProxyTrue(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	rl := IPRateLimit(limiter, time.Minute, nil, true)
	mux := stdhttp.NewServeMux()
	mux.Handle("/x", rl(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	})))

	send := func(xff string) int {
		req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
		req.RemoteAddr = "10.0.0.1:1234" // single proxy
		req.Header.Set("X-Forwarded-For", xff)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	// Two distinct XFF leftmosts each get a fresh bucket of 1 → both 200.
	if got := send("1.2.3.4"); got != stdhttp.StatusOK {
		t.Fatalf("first hit from 1.2.3.4: got %d want 200", got)
	}
	if got := send("5.6.7.8"); got != stdhttp.StatusOK {
		t.Fatalf("first hit from 5.6.7.8: got %d want 200", got)
	}
	// Second hit from 1.2.3.4 exhausts that bucket → 429.
	if got := send("1.2.3.4"); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("second hit from 1.2.3.4: got %d want 429", got)
	}
	// 5.6.7.8 still has its own (now-exhausted) bucket too.
	if got := send("5.6.7.8"); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("second hit from 5.6.7.8: got %d want 429", got)
	}
}

// Symmetric to the above: with trustedProxy=false the bucket keys on
// RemoteAddr, so two distinct XFF leftmosts behind one proxy share a
// single bucket and the second hit 429s. This nails down the safe
// default — XFF is ignored when the flag is unset.
func TestIPRateLimitIgnoresXFFWhenTrustedProxyFalse(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	rl := IPRateLimit(limiter, time.Minute, nil, false)
	mux := stdhttp.NewServeMux()
	mux.Handle("/x", rl(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	})))

	send := func(xff string) int {
		req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", xff)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	if got := send("1.2.3.4"); got != stdhttp.StatusOK {
		t.Fatalf("first hit: got %d want 200", got)
	}
	// Same proxy RemoteAddr, different XFF → still the same bucket.
	if got := send("5.6.7.8"); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("second hit (XFF must be ignored): got %d want 429", got)
	}
}

// Phase 8 — UserRateLimit keys on the authenticated user id from the
// request context. Two distinct ids each get their own bucket; one
// id's exhaustion does not block another.
func TestUserRateLimitKeysOnUserID(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	rl := UserRateLimit(limiter, time.Minute, nil, false)
	mux := stdhttp.NewServeMux()
	mux.Handle("/x", rl(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	})))
	send := func(uid string) int {
		req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
		req = req.WithContext(WithUserID(req.Context(), uid))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}
	if got := send("alice"); got != stdhttp.StatusOK {
		t.Fatalf("alice first: got %d want 200", got)
	}
	if got := send("bob"); got != stdhttp.StatusOK {
		t.Fatalf("bob first (separate bucket): got %d want 200", got)
	}
	if got := send("alice"); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("alice second (bucket exhausted): got %d want 429", got)
	}
	if got := send("bob"); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("bob second: got %d want 429", got)
	}
}

// Phase 8 — UserRateLimit returns the standard envelope on rejection.
func TestUserRateLimitEnvelopeOnRejection(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	rl := UserRateLimit(limiter, time.Minute, nil, false)
	h := rl(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	}))
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
		req = req.WithContext(WithUserID(req.Context(), "alice"))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := send(); rr.Code != stdhttp.StatusOK {
		t.Fatalf("first: got %d want 200", rr.Code)
	}
	rr := send()
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("second: got %d want 429", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Errorf("Retry-After header missing on 429")
	}
	var env envelope
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != CodeRateLimited {
		t.Fatalf("envelope: %+v", env)
	}
}

// Phase 8 — without a user id in context the middleware passes through.
// Lets unauth'd traffic flow to inner handlers (which themselves return
// 401); without this gate the limiter would reject the empty key.
func TestUserRateLimitPassesThroughWithoutUserID(t *testing.T) {
	limiter := ratelimit.NewIPLimiter(ratelimit.IPLimiterConfig{Burst: 1, Refill: time.Hour})
	rl := UserRateLimit(limiter, time.Minute, nil, false)
	h := rl(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	}))
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != stdhttp.StatusOK {
			t.Fatalf("attempt %d: got %d want 200 (no user id should bypass limiter)", i, rr.Code)
		}
	}
}

// clientIP is the helper IPRateLimit + the audit log share. Pin its
// branch table directly: the wiring above exercises the integration,
// these table-driven cases lock in each branch's intent.
func TestClientIPHonorsTrustedProxyFlag(t *testing.T) {
	cases := []struct {
		name         string
		remote       string
		xff          string
		trustedProxy bool
		want         string
	}{
		{name: "default: trust RemoteAddr", remote: "1.2.3.4:5678", xff: "9.9.9.9", trustedProxy: false, want: "1.2.3.4"},
		{name: "trusted: leftmost XFF wins", remote: "10.0.0.1:5678", xff: "1.2.3.4, 5.6.7.8", trustedProxy: true, want: "1.2.3.4"},
		{name: "trusted: garbage XFF falls back", remote: "10.0.0.1:5678", xff: "garbage", trustedProxy: true, want: "10.0.0.1"},
		{name: "trusted: empty XFF falls back", remote: "10.0.0.1:5678", xff: "", trustedProxy: true, want: "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(stdhttp.MethodGet, "/x", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(req, tc.trustedProxy); got != tc.want {
				t.Fatalf("clientIP(remote=%q, xff=%q, trusted=%v): got %q, want %q",
					tc.remote, tc.xff, tc.trustedProxy, got, tc.want)
			}
		})
	}
}
