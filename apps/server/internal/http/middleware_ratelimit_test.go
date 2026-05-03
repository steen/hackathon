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
// in front of /api/login and /api/register, mirroring main.go.
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
	loginRL := IPRateLimit(ratelimit.NewIPLimiter(loginCfg), 5*time.Minute, h.AuditSink())
	registerRL := IPRateLimit(ratelimit.NewIPLimiter(registerCfg), 15*time.Minute, h.AuditSink())
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/register", registerRL(stdhttp.HandlerFunc(h.Register)))
	mux.Handle("/api/login", loginRL(stdhttp.HandlerFunc(h.Login)))
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
		rr := f.post(t, "/api/login", body, "9.9.9.9")
		if rr.Code == stdhttp.StatusTooManyRequests {
			t.Fatalf("attempt %d/10 already 429; SEC-5 requires the 11th to be the first 429", i)
		}
	}
	rr := f.post(t, "/api/login", body, "9.9.9.9")
	if rr.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("11th login attempt within 5 min from one IP must return 429 (SEC-5); got %d", rr.Code)
	}
}

func TestIPRateLimitBlocksRegisterAfterBurst(t *testing.T) {
	f := newRateLimitFixture(t, ratelimit.LoginIPConfig(), ratelimit.RegisterIPConfig(), nil)
	defer f.close()

	for i := 1; i <= 5; i++ {
		rr := f.post(t, "/api/register", map[string]string{
			"username":    "user-x",
			"password":    "correct-horse-battery",
			"invite_code": "WRONG", // 403 doesn't refund the bucket
		}, "8.8.8.8")
		if rr.Code == stdhttp.StatusTooManyRequests {
			t.Fatalf("attempt %d should fit within burst=5", i)
		}
	}
	rr := f.post(t, "/api/register", map[string]string{
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
	if rr := f.post(t, "/api/login", body, "7.7.7.7"); rr.Code == stdhttp.StatusTooManyRequests {
		t.Fatal("first attempt should not be limited")
	}
	rr := f.post(t, "/api/login", body, "7.7.7.7")
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
	uc := ratelimit.LoginUserConfig()
	// Squeeze the gate so a single failure produces an observable
	// retry-after; this lets us verify Reset() actually clears it.
	uc.GraceFailures = 0
	uc.Step = 50 * time.Millisecond
	uc.MaxDelay = 100 * time.Millisecond
	f := newRateLimitFixture(t, ratelimit.LoginIPConfig(), ratelimit.RegisterIPConfig(), &uc)
	defer f.close()

	if rr := f.post(t, "/api/register", map[string]string{
		"username":    "alice",
		"password":    "correct-horse-battery",
		"invite_code": "INVITE-OK",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusCreated {
		t.Fatalf("register: %d", rr.Code)
	}

	// One bad password registers a failure on the user limiter.
	if rr := f.post(t, "/api/login", map[string]string{
		"username": "alice", "password": "wrong-password-xyz",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("bad login status: %d", rr.Code)
	}
	// Wait past the gate, then succeed → should reset the counter.
	time.Sleep(120 * time.Millisecond)
	if rr := f.post(t, "/api/login", map[string]string{
		"username": "alice", "password": "correct-horse-battery",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusOK {
		t.Fatalf("good login status: %d", rr.Code)
	}
	// One subsequent failure should land within the grace (here =0)
	// window with a fresh failure count of 1, so the Allow() check on
	// the *next* attempt is what gates. Verify by a wrong password,
	// then immediately retry — if Reset didn't fire, this would gate
	// instantly with a multi-step delay; instead it is a single Step.
	if rr := f.post(t, "/api/login", map[string]string{
		"username": "alice", "password": "wrong-again-here",
	}, "1.1.1.1"); rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("post-reset bad login: %d", rr.Code)
	}
	rr := f.post(t, "/api/login", map[string]string{
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
	_ = f.post(t, "/api/login", body, "5.5.5.5")
	rr := f.post(t, "/api/login", body, "5.5.5.5")
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
