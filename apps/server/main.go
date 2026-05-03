package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/config"
	appdb "hackathon/apps/server/internal/db"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/ratelimit"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/wsapi"
)

const (
	portEnv           = "CHAT_SERVER_PORT"
	dbPathEnv         = "CHAT_DB_PATH"
	jwtSecretEnv      = "CHAT_JWT_SECRET" //nolint:gosec // G101 false positive: env var name, not a credential.
	inviteCodeEnv     = "CHAT_INVITE_CODE"
	allowedOriginsEnv = "CHAT_ALLOWED_ORIGINS"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

// repository is the process-wide SQLite handle for later phase-1 features
// (auth, channels, messages). Nil when CHAT_DB_PATH is unset (phase-0 boot
// path, e.g. scripts/smoke.sh, must not require a SQLite file on disk).
//
//nolint:unused // wired into run() once phase-1 handlers land.
var repository *repo.Repo

func run() error {
	cfg := config.Load()
	checks, err := cfg.Validate()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	for _, ch := range checks {
		log.Printf("config check ok: %s", ch.Name)
	}

	listenAddr, err := resolveListenAddr(cfg.ListenAddr, os.Getenv(portEnv))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if dbPath := os.Getenv(dbPathEnv); dbPath != "" {
		sqlDB, err := appdb.Open(dbPath)
		if err != nil {
			return fmt.Errorf("db open: %w", err)
		}
		defer func() { _ = sqlDB.Close() }()
		migCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := appdb.Apply(migCtx, sqlDB); err != nil {
			cancel()
			return fmt.Errorf("db migrate: %w", err)
		}
		cancel()
		repository, err = repo.New(sqlDB)
		if err != nil {
			return fmt.Errorf("repo init: %w", err)
		}
		log.Printf("db ready at %q", dbPath)
	}

	h := hub.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(h))
	allowedOrigins := parseAllowedOrigins(os.Getenv(allowedOriginsEnv))
	log.Printf("config check ok: %s parsed %d origin pattern(s)", allowedOriginsEnv, len(allowedOrigins))
	wsCfg := wsapi.Config{OriginPatterns: allowedOrigins}
	if repository != nil {
		wsCfg.ChannelLookup = repository.ChannelExists
	}
	var tickets *auth.TicketStore

	if repository != nil {
		jwtSecret := []byte(os.Getenv(jwtSecretEnv))
		if len(jwtSecret) == 0 {
			return fmt.Errorf("config: %s must be set when %s is set", jwtSecretEnv, dbPathEnv)
		}
		tickets = auth.NewTicketStore()
		loginIPLimiter := ratelimit.NewIPLimiter(ratelimit.LoginIPConfig())
		registerIPLimiter := ratelimit.NewIPLimiter(ratelimit.RegisterIPConfig())
		userLimiter := ratelimit.NewUserLimiter(ratelimit.LoginUserConfig())
		ah := httpapi.NewAuthHandlers(httpapi.AuthDeps{
			DB:          repository.DB(),
			Tickets:     tickets,
			SigningKey:  jwtSecret,
			InviteCode:  os.Getenv(inviteCodeEnv),
			UserLimiter: userLimiter,
		})
		require := auth.RequireJWT(auth.MiddlewareConfig{
			SigningKey:        jwtSecret,
			Lookup:            ah.LookupUserInfo,
			WriteUnauthorized: httpapi.WriteUnauthorized,
		})
		loginRL := httpapi.IPRateLimit(loginIPLimiter, 5*time.Minute, ah.AuditSink())
		registerRL := httpapi.IPRateLimit(registerIPLimiter, 15*time.Minute, ah.AuditSink())
		mux.Handle("/api/auth/register", registerRL(http.HandlerFunc(ah.Register)))
		mux.Handle("/api/auth/login", loginRL(http.HandlerFunc(ah.Login)))
		mux.Handle("/api/auth/me", require(http.HandlerFunc(ah.Me)))
		mux.Handle("/api/auth/logout", require(http.HandlerFunc(ah.Logout)))
		mux.Handle("/api/auth/ws-ticket", require(http.HandlerFunc(ah.WSTicket)))

		ch := httpapi.NewChannelsHandlers(httpapi.ChannelsDeps{
			Repo: repository,
			Hub:  h,
		})
		msg := httpapi.NewMessagesHandlers(httpapi.MessagesDeps{
			Repo: repository,
			Hub:  h,
		})
		ch.Routes(mux, require, msg)

		presence := httpapi.NewPresenceHandlers(httpapi.PresenceDeps{
			Hub: h,
			DB:  repository.DB(),
		})
		mux.Handle("GET /api/presence", require(http.HandlerFunc(presence.List)))
	}

	mux.HandleFunc("/ws", wsapi.Handler(h, tickets, wsCfg))

	// ReadHeaderTimeout caps a slow upgrade handshake (Slowloris). IdleTimeout
	// caps post-upgrade silence on idle keep-alives. WriteTimeout stays zero —
	// it would fight the WebSocket upgrade's hijacked connection.
	// Middleware order (outer → inner):
	//   SecurityHeaders      → SEC-10 baseline headers on every response
	//   RequestIDMiddleware  → assigns X-Request-Id, plumbs ctx
	//   AccessLog            → emits one log line per request (uses ctx)
	//   Recover              → catches panics, writes generic 500
	//   BodyCap              → caps request body to RESTBodyLimit
	//   mux                  → handler dispatch
	// SecurityHeaders is outermost so even error responses written by inner
	// middleware (Recover's 500, BodyCap's 413) carry the headers.
	// statusRecorder.Hijack forwards the Hijacker interface so /ws upgrade
	// still works through this chain.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httpapi.SecurityHeaders(httpapi.RequestIDMiddleware(httpapi.AccessLog(httpapi.Recover(httpapi.BodyCap(mux))))),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("chat server listening on %s", listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	return nil
}

// parseAllowedOrigins splits a comma-separated CHAT_ALLOWED_ORIGINS
// value into the OriginPatterns shape coder/websocket expects. Empty
// or whitespace-only entries are dropped so a stray trailing comma is
// not treated as a wildcard.
func parseAllowedOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveListenAddr returns the address the HTTP server should bind. cfg is
// the validated CHAT_LISTEN_ADDR (default 127.0.0.1:8080); portOverride is
// the legacy CHAT_SERVER_PORT env var, kept for compatibility with existing
// tests and operator habits. When portOverride is set it replaces the port
// component of cfg without changing the host, so SEC-2 (loopback unless
// overridden) still holds.
func resolveListenAddr(cfg, portOverride string) (string, error) {
	if portOverride == "" {
		return cfg, nil
	}
	n, err := strconv.Atoi(portOverride)
	if err != nil {
		return "", fmt.Errorf("%s=%q is not a valid integer: %w", portEnv, portOverride, err)
	}
	if n < 1 || n > 65535 {
		return "", fmt.Errorf("%s=%d is out of range [1,65535]", portEnv, n)
	}
	host, _, err := net.SplitHostPort(cfg)
	if err != nil {
		return "", fmt.Errorf("config: CHAT_LISTEN_ADDR=%q is not host:port: %w", cfg, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}
