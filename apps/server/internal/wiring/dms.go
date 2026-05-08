package wiring

import (
	"log/slog"
	"net/http"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerDMs wires /api/dms, /api/dms/{id}/messages, and
// /api/dms/{id}/read. require is the JWT middleware constructed by
// registerAuth — every DM route is gated through it.
//
// POST /api/dms/{id}/messages is wrapped in a per-user dm-write
// token-bucket limiter (decision-log L17, default Burst=10 / Refill=1m
// from ratelimit.DMWriteUserConfig). POST /api/dms/{id}/read is wrapped
// in the per-user read-mark limiter (L17, default Burst=50 / Refill=1m
// from ratelimit.ReadMarkUserConfig). Order in both cases: JWT → user
// limiter → handler, so each limiter reads the user id from the
// context the JWT middleware set — same shape as the per-user
// channel-write limiter in registerChannels. find-or-create (POST
// /api/dms) and the listing endpoints carry no extra bucket; the
// per-IP bucket the auth feature installs upstream still applies to
// the request as a whole.
func registerDMs(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	dms := httpapi.NewDMsHandlers(httpapi.DMsDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})

	trustedProxy := config.LoadTrustedProxy()
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())

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
	writeLimit := httpapi.UserRateLimit(writeLimiter, writeCfg.Refill, auditSink, trustedProxy)

	readCfg := ratelimit.ReadMarkUserConfigFromEnv()
	readDefault := ratelimit.ReadMarkUserConfig()
	if readCfg.Burst != readDefault.Burst || readCfg.Refill != readDefault.Refill {
		slog.Warn("per-user read-mark rate limit overridden from decision-log L17 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvReadMarkBurst,
			"env_refill", ratelimit.EnvReadMarkRefill,
			"burst", readCfg.Burst,
			"refill", readCfg.Refill,
			"default_burst", readDefault.Burst,
			"default_refill", readDefault.Refill,
		)
	}
	slog.Info("per-user read-mark rate limit active",
		"burst", readCfg.Burst,
		"refill", readCfg.Refill,
	)
	readLimiter := ratelimit.NewIPLimiter(readCfg)
	readLimit := httpapi.UserRateLimit(readLimiter, readCfg.Refill, auditSink, trustedProxy)

	dms.Routes(mux, require, writeLimit, readLimit)
}
