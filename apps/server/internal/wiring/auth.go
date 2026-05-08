package wiring

import (
	"log/slog"
	"net/http"
	"time"

	"hackathon/apps/server/internal/auth"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerAuth wires the /api/auth/* surface and constructs the
// RequireJWT middleware that other features wrap around. Returns
// the middleware + ticket store so the WS and channels features can
// reuse them without rebuilding the dependency graph.
//
// trustedProxy is the parsed CHAT_TRUSTED_PROXY flag (PRD §9 / §11),
// threaded through to the auth handlers (clientIP for audit rows) and
// the IP rate-limit middleware (per-IP bucket key). Without this the
// rate-limit bucket collapses to one key behind a reverse proxy.
func registerAuth(mux *http.ServeMux, deps Deps, trustedProxy bool) authBundle {
	tickets := auth.NewTicketStore()

	loginIPLimiter := ratelimit.NewIPLimiter(ratelimit.LoginIPConfig())
	registerIPCfg := ratelimit.RegisterIPConfigFromEnv()
	registerIPDefault := ratelimit.RegisterIPConfig()
	if registerIPCfg.Burst != registerIPDefault.Burst || registerIPCfg.Refill != registerIPDefault.Refill {
		slog.Warn("per-IP register rate limit loosened from PRD §9 default; ensure this is a test/dev override",
			"env_burst", ratelimit.EnvRegisterBurst,
			"env_refill", ratelimit.EnvRegisterRefill,
			"burst", registerIPCfg.Burst,
			"refill", registerIPCfg.Refill,
			"default_burst", registerIPDefault.Burst,
			"default_refill", registerIPDefault.Refill,
		)
	}
	registerIPLimiter := ratelimit.NewIPLimiter(registerIPCfg)
	userLimiter := ratelimit.NewUserLimiter(ratelimit.LoginUserConfig())

	ah := httpapi.NewAuthHandlers(httpapi.AuthDeps{
		DB:           deps.Repo.DB(),
		Tickets:      tickets,
		SigningKey:   deps.JWTSecret,
		InviteCode:   deps.InviteCode,
		UserLimiter:  userLimiter,
		TrustedProxy: trustedProxy,
	})

	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        deps.JWTSecret,
		Lookup:            ah.LookupUserInfo,
		WriteUnauthorized: httpapi.WriteUnauthorized,
		WithUserID:        httpapi.WithUserID,
	})

	loginRL := httpapi.IPRateLimit(loginIPLimiter, 5*time.Minute, ah.AuditSink(), trustedProxy)
	registerRL := httpapi.IPRateLimit(registerIPLimiter, 15*time.Minute, ah.AuditSink(), trustedProxy)

	mux.Handle("/api/auth/register", registerRL(http.HandlerFunc(ah.Register)))
	mux.Handle("/api/auth/login", loginRL(http.HandlerFunc(ah.Login)))
	mux.Handle("/api/auth/me", require(http.HandlerFunc(ah.Me)))
	mux.Handle("/api/auth/logout", require(http.HandlerFunc(ah.Logout)))
	mux.Handle("/api/auth/ws-ticket", require(http.HandlerFunc(ah.WSTicket)))

	return authBundle{
		Require: require,
		Tickets: tickets,
	}
}
