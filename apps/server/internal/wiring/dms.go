package wiring

import (
	"log/slog"
	"net/http"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerDMs wires /api/dms and /api/dms/{id}/messages. require is the
// JWT middleware constructed by registerAuth — every DM route is gated
// through it.
//
// POST /api/dms/{id}/messages is also wrapped in a per-user dm-write
// token-bucket limiter (decision-log L17, default Burst=10 / Refill=1m
// from ratelimit.DMWriteUserConfig). Order: JWT → user limiter →
// handler, so the limiter reads the user id from the context the JWT
// middleware set — same shape as the per-user channel-write limiter
// in registerChannels. find-or-create (POST /api/dms) and the listing
// endpoints carry no extra bucket; the per-IP bucket the auth feature
// installs upstream still applies to the request as a whole.
func registerDMs(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	dms := httpapi.NewDMsHandlers(httpapi.DMsDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})

	writeCfg := ratelimit.DMWriteUserConfigFromEnv()
	writeDefault := ratelimit.DMWriteUserConfig()
	if writeCfg.Burst != writeDefault.Burst || writeCfg.Refill != writeDefault.Refill {
		slog.Warn("per-user dm-write rate limit overridden from decision-log L17 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvDMWriteBurst,
			"env_refill", ratelimit.EnvDMWriteRefill,
			"burst", writeCfg.Burst,
			"refill", writeCfg.Refill,
			"default_burst", writeDefault.Burst,
			"default_refill", writeDefault.Refill,
		)
	}
	slog.Info("per-user dm-write rate limit active",
		"burst", writeCfg.Burst,
		"refill", writeCfg.Refill,
	)
	writeLimiter := ratelimit.NewIPLimiter(writeCfg)
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())
	writeLimit := httpapi.UserRateLimit(writeLimiter, writeCfg.Refill, auditSink, config.LoadTrustedProxy())

	dms.Routes(mux, require, writeLimit)
}
