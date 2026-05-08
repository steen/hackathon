package wiring

import (
	"log/slog"
	"net/http"
	"time"

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
	writeLimit := httpapi.UserRateLimit(writeLimiter, time.Minute)

	ch.Routes(mux, require, writeLimit, msg)
}
