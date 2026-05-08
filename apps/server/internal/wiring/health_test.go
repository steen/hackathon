package wiring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/repo"
	"hackathon/migrations"
)

// healthEnvelope mirrors the {ok,data,error} envelope contract from
// PRD §10 so tests assert against the wire shape directly.
type healthEnvelope struct {
	OK   bool `json:"ok"`
	Data *struct {
		Status string `json:"status"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestHealthz_Always200(t *testing.T) {
	deps := Deps{Hub: hub.New(), Repo: newHealthRepo(t)}

	mux := http.NewServeMux()
	registerHealth(mux, deps)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rec.Code, rec.Body.String())
	}
	env := decodeHealthEnvelope(t, rec.Body.Bytes())
	if !env.OK || env.Data == nil || env.Data.Status != "ok" || env.Error != nil {
		t.Fatalf("envelope: got %+v, want ok=true data.status=ok error=nil", env)
	}
}

func TestReadyz_HealthyRepo200(t *testing.T) {
	deps := Deps{Hub: hub.New(), Repo: newHealthRepo(t)}

	mux := http.NewServeMux()
	registerHealth(mux, deps)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rec.Code, rec.Body.String())
	}
	env := decodeHealthEnvelope(t, rec.Body.Bytes())
	if !env.OK || env.Data == nil || env.Data.Status != "ok" || env.Error != nil {
		t.Fatalf("envelope: got %+v, want ok=true data.status=ok error=nil", env)
	}
}

func TestReadyz_PingFailure503(t *testing.T) {
	r := newHealthRepo(t)
	// Closing the underlying *sql.DB makes every subsequent PingContext
	// return sql.ErrConnDone — the cheapest way to drive the failure
	// arm without injecting a fake driver.
	if err := r.DB().Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	deps := Deps{Hub: hub.New(), Repo: r}
	mux := http.NewServeMux()
	registerHealth(mux, deps)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503, body=%s", rec.Code, rec.Body.String())
	}
	env := decodeHealthEnvelope(t, rec.Body.Bytes())
	if env.OK || env.Error == nil {
		t.Fatalf("envelope: got %+v, want ok=false error!=nil", env)
	}
	if env.Error.Code != "not_ready" {
		t.Fatalf("error.code: got %q want %q", env.Error.Code, "not_ready")
	}
	if env.Data != nil {
		t.Fatalf("data: got %+v want nil", env.Data)
	}
}

// newHealthRepo builds a Repo backed by a fresh SQLite tempfile with
// migrations applied. Mirrors newTestDeps's repo construction but
// without the auth/JWT plumbing, since /readyz only exercises DB().Ping.
func newHealthRepo(t *testing.T) *repo.Repo {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := appdb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("appdb.Open: %v", err)
	}
	if err := appdb.ApplyFS(context.Background(), sqlDB, migrations.FS); err != nil {
		t.Fatalf("appdb.ApplyFS: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	r, err := repo.New(sqlDB)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	return r
}

func decodeHealthEnvelope(t *testing.T, raw []byte) healthEnvelope {
	t.Helper()
	var env healthEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v (raw=%s)", err, string(raw))
	}
	return env
}
