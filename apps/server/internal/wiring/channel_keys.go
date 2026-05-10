package wiring

import (
	"log/slog"
	"net/http"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerChannelKeys wires the standalone keys-RPC pair:
//
//   - POST /api/channels/{id}/keys                      — bootstrap | fill-in | rotation
//   - GET  /api/channels/{id}/members/wraps-needed      — L22 lazy-wrap input
//
// Per-user rate limits land here so a misbehaving client cannot loop
// either path: the wraps-needed bucket from #980 (e2e decision-log
// L31 + L36) gates the GET; the channel-write bucket (already in use
// for the channels feature) gates the POST so its allowance shares
// with channel-create + invite. require is the JWT middleware
// constructed by registerAuth — same pattern as registerChannels.
func registerChannelKeys(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	keys := httpapi.NewChannelKeysHandlers(httpapi.ChannelKeysDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})

	wrapsCfg := ratelimit.WrapsNeededUserConfigFromEnv()
	wrapsDefault := ratelimit.WrapsNeededUserConfig()
	if wrapsCfg.Burst != wrapsDefault.Burst || wrapsCfg.Refill != wrapsDefault.Refill {
		slog.Warn("per-user wraps-needed rate limit overridden from L31/L36 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvWrapsNeededBurst,
			"env_refill", ratelimit.EnvWrapsNeededRefill,
			"burst", wrapsCfg.Burst,
			"refill", wrapsCfg.Refill,
			"default_burst", wrapsDefault.Burst,
			"default_refill", wrapsDefault.Refill,
		)
	}
	slog.Info("per-user wraps-needed rate limit active",
		"burst", wrapsCfg.Burst,
		"refill", wrapsCfg.Refill,
	)
	wrapsLimiter := ratelimit.NewIPLimiter(wrapsCfg)
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())
	wrapsLimit := httpapi.UserRateLimit(wrapsLimiter, wrapsCfg.Refill, auditSink, config.LoadTrustedProxy())

	// Re-use the channel-write bucket for the POST /keys path. The
	// keys-RPC is a wrap-insert path and shares the same friend-scale
	// allowance contract as channel-create / channel-rename / invite;
	// a separate bucket would invite drift between two related limits.
	writeCfg := ratelimit.ChannelWriteUserConfigFromEnv()
	writeLimiter := ratelimit.NewIPLimiter(writeCfg)
	writeLimit := httpapi.UserRateLimit(writeLimiter, writeCfg.Refill, auditSink, config.LoadTrustedProxy())

	keys.Routes(mux, require, wrapsLimit, writeLimit)
}
