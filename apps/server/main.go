package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hackathon/apps/server/internal/auth"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/hub"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/wsapi"
)

const (
	defaultPort       = 8080
	portEnv           = "CHAT_SERVER_PORT"
	dbPathEnv         = "CHAT_DB_PATH"
	jwtSecretEnv      = "CHAT_JWT_SECRET"
	inviteCodeEnv     = "CHAT_INVITE_CODE"
	allowedOriginsEnv = "CHAT_ALLOWED_ORIGINS"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
)

// repository is a process-wide handle that later phase-1 features (auth,
// channels, messages) will use to reach SQLite. Kept package-level so the
// startup wiring lives in one place; nil when CHAT_DB_PATH is unset (phase-0
// boot path, e.g. scripts/smoke.sh, must not require a SQLite file on disk).
var repository *repo.Repo

func main() {
	port, err := resolvePort(os.Getenv(portEnv))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if dbPath := os.Getenv(dbPathEnv); dbPath != "" {
		sqlDB, err := appdb.Open(dbPath)
		if err != nil {
			log.Fatalf("db open: %v", err)
		}
		defer func() { _ = sqlDB.Close() }()
		migCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := appdb.Apply(migCtx, sqlDB); err != nil {
			cancel()
			log.Fatalf("db migrate: %v", err)
		}
		cancel()
		repository, err = repo.New(sqlDB)
		if err != nil {
			log.Fatalf("repo init: %v", err)
		}
		log.Printf("db ready at %s", dbPath)
	}

	h := hub.New()
	mux := http.NewServeMux()
	wsCfg := wsapi.Config{OriginPatterns: parseAllowedOrigins(os.Getenv(allowedOriginsEnv))}
	var tickets *auth.TicketStore

	if repository != nil {
		jwtSecret := []byte(os.Getenv(jwtSecretEnv))
		if len(jwtSecret) == 0 {
			log.Fatalf("config: %s must be set when %s is set", jwtSecretEnv, dbPathEnv)
		}
		tickets = auth.NewTicketStore()
		ah := httpapi.NewAuthHandlers(httpapi.AuthDeps{
			DB:         repository.DB(),
			Tickets:    tickets,
			SigningKey: jwtSecret,
			InviteCode: os.Getenv(inviteCodeEnv),
		})
		require := auth.RequireJWT(auth.MiddlewareConfig{
			SigningKey:        jwtSecret,
			Lookup:            ah.LookupUserInfo,
			WriteUnauthorized: httpapi.WriteUnauthorized,
		})
		mux.HandleFunc("/api/register", ah.Register)
		mux.HandleFunc("/api/login", ah.Login)
		mux.Handle("/api/me", require(http.HandlerFunc(ah.Me)))
		mux.Handle("/api/logout", require(http.HandlerFunc(ah.Logout)))
		mux.Handle("/api/ws-ticket", require(http.HandlerFunc(ah.WSTicket)))
	}

	mux.HandleFunc("/ws", wsapi.Handler(h, tickets, wsCfg))

	// ReadHeaderTimeout caps a slow upgrade handshake (Slowloris). IdleTimeout
	// caps post-upgrade silence on idle keep-alives. WriteTimeout stays zero —
	// it would fight the WebSocket upgrade's hijacked connection.
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("chat server listening on :%d", port)
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

// resolvePort parses raw as a TCP port number. Empty raw returns defaultPort.
// Anything outside [1, 65535] is reported as a config error so operators see a
// clear startup failure instead of a downstream listen-syscall message.
func resolvePort(raw string) (int, error) {
	if raw == "" {
		return defaultPort, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a valid integer: %w", portEnv, raw, err)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s=%d is out of range [1,65535]", portEnv, n)
	}
	return n, nil
}
