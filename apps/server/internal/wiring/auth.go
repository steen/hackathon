package wiring

import (
	"log"
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
// Skips registration entirely when Deps.Repo is nil — the no-DB
// boot path used by smoke tests has no users to authenticate.
func registerAuth(mux *http.ServeMux, deps Deps) authBundle {
	if deps.Repo == nil {
		return authBundle{}
	}

	tickets := auth.NewTicketStore()

	loginIPLimiter := ratelimit.NewIPLimiter(ratelimit.LoginIPConfig())
	registerIPCfg := ratelimit.RegisterIPConfigFromEnv()
	registerIPDefault := ratelimit.RegisterIPConfig()
	if registerIPCfg.Burst != registerIPDefault.Burst || registerIPCfg.Refill != registerIPDefault.Refill {
		log.Printf("WARN: %s/%s loosen the per-IP register rate limit (Burst=%d, Refill=%s vs PRD §9 default Burst=%d, Refill=%s); ensure this is a test/dev override",
			ratelimit.EnvRegisterBurst, ratelimit.EnvRegisterRefill,
			registerIPCfg.Burst, registerIPCfg.Refill,
			registerIPDefault.Burst, registerIPDefault.Refill)
	}
	registerIPLimiter := ratelimit.NewIPLimiter(registerIPCfg)
	userLimiter := ratelimit.NewUserLimiter(ratelimit.LoginUserConfig())

	ah := httpapi.NewAuthHandlers(httpapi.AuthDeps{
		DB:          deps.Repo.DB(),
		Tickets:     tickets,
		SigningKey:  deps.JWTSecret,
		InviteCode:  deps.InviteCode,
		UserLimiter: userLimiter,
	})

	require := auth.RequireJWT(auth.MiddlewareConfig{
		SigningKey:        deps.JWTSecret,
		Lookup:            ah.LookupUserInfo,
		WriteUnauthorized: httpapi.WriteUnauthorized,
		WithUserID:        httpapi.WithUserID,
	})

	loginRL := httpapi.IPRateLimit(loginIPLimiter, 5*time.Minute, ah.AuditSink())
	registerRL := httpapi.IPRateLimit(registerIPLimiter, 15*time.Minute, ah.AuditSink())

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
