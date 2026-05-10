package wiring

import (
	"net/http"

	"hackathon/apps/server/internal/config"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/ratelimit"
)

// registerChannelMembers wires the /api/channels/{id}/members surface
// (decision-log §6, §9, §10). require is the JWT middleware constructed
// by registerAuth — every member route is gated through it.
//
// Invite + kick are throttled with the per-user channel-write bucket so
// a flood of invites shares the rate-limit surface as channel renames
// (PRD §9). Listing is read-only and unthrottled; the JWT gate is
// already in place.
func registerChannelMembers(mux *http.ServeMux, deps Deps, require func(http.Handler) http.Handler) {
	members := httpapi.NewMembersHandlers(httpapi.MembersDeps{
		Repo: deps.Repo,
		Hub:  deps.Hub,
	})
	writeCfg := ratelimit.ChannelWriteUserConfigFromEnv()
	limiter := ratelimit.NewIPLimiter(writeCfg)
	auditSink := httpapi.NewRateLimitAuditSink(deps.Repo.DB())
	writeLimit := httpapi.UserRateLimit(limiter, writeCfg.Refill, auditSink, config.LoadTrustedProxy())
	members.Routes(mux, require, writeLimit)
}
