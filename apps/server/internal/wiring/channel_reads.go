package wiring

import (
	"log/slog"
	"net/http"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerChannelReads wires POST /api/channels/{id}/read with the JWT
// middleware and the per-user read-mark token bucket from
// ratelimit.ReadMarkUserConfig (decision log L17 / sub-issue G0).
//
// Order: JWT → read-mark → handler. The limiter reads the user id from
// the context the JWT middleware set; same shape as the channel-write
// limiter wired in registerChannels (apps/server/internal/wiring/channels.go).
//
// The audit sink reuses the same RateLimitAuditSink the auth feature
// constructs from deps.Repo.DB() so 429 paths share one writer + one
// event kind. trustedProxy comes from the same env flag wiring.Build
// reads, matching the access-log IP.
func registerChannelReads(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	cr := httpapi.NewChannelReadsHandlers(httpapi.ChannelReadsDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})

	cfg := ratelimit.ReadMarkUserConfigFromEnv()
	defaultCfg := ratelimit.ReadMarkUserConfig()
	if cfg.Burst != defaultCfg.Burst || cfg.Refill != defaultCfg.Refill {
		slog.Warn("per-user read-mark rate limit overridden from decision-log L17 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvReadMarkBurst,
			"env_refill", ratelimit.EnvReadMarkRefill,
			"burst", cfg.Burst,
			"refill", cfg.Refill,
			"default_burst", defaultCfg.Burst,
			"default_refill", defaultCfg.Refill,
		)
	}
	slog.Info("per-user read-mark rate limit active",
		"burst", cfg.Burst,
		"refill", cfg.Refill,
	)
	limiter := ratelimit.NewIPLimiter(cfg)
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())
	readMark := httpapi.UserRateLimit(limiter, cfg.Refill, auditSink, config.LoadTrustedProxy())

	cr.Routes(mux, require, readMark)
}
