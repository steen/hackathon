package wiring

import (
	"log/slog"
	"net/http"
	"time"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerChannels wires /api/channels and /api/channels/{id}/messages.
// require is the JWT middleware constructed by registerAuth — every
// channel + message route is gated through it.
//
// The channel-write surface (POST + PATCH /api/channels) is also wrapped
// in a per-user token-bucket limiter (PRD §9). Order: JWT → user limiter
// → handler, so the limiter reads the user id from the context the JWT
// middleware set.
//
// Skips registration when Deps.Repo or require is nil; both come from
// the auth feature which itself skips when there is no DB.
func registerChannels(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	if deps.Repo == nil || require == nil {
		return
	}

	ch := httpapi.NewChannelsHandlers(httpapi.ChannelsDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})
	msg := httpapi.NewMessagesHandlers(httpapi.MessagesDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})

	writeCfg := ratelimit.ChannelWriteUserConfigFromEnv()
	writeDefault := ratelimit.ChannelWriteUserConfig()
	if writeCfg.Burst != writeDefault.Burst || writeCfg.Refill != writeDefault.Refill {
		slog.Warn("per-user channel-write rate limit overridden from PRD §9 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvChannelWriteBurst,
			"env_refill", ratelimit.EnvChannelWriteRefill,
			"burst", writeCfg.Burst,
			"refill", writeCfg.Refill,
			"default_burst", writeDefault.Burst,
			"default_refill", writeDefault.Refill,
		)
	}
	writeLimiter := ratelimit.NewIPLimiter(writeCfg)
	// Mirror IPRateLimit's audit story for the per-user channel-write
	// limiter: rejected attempts append a row to auth_events keyed on
	// the user id (#883). Reuse the same RateLimitAuditSink the auth
	// feature constructs from its own *sql.DB so both 429 paths share
	// one writer + one event kind. trustedProxy comes from the same env
	// flag wiring.Build reads, so the audit IP matches the access-log IP.
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())
	writeLimit := httpapi.UserRateLimit(writeLimiter, time.Minute, auditSink, config.LoadTrustedProxy())

	ch.Routes(mux, require, writeLimit, msg)
}
