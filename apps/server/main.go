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
	"syscall"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/config"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/httpx"
	"hackathon/apps/server/internal/hub"
	httpapi "hackathon/apps/server/internal/http"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/wsapi"
)

const (
	portEnv           = "CHAT_SERVER_PORT"
	dbPathEnv         = "CHAT_DB_PATH"
	jwtSecretEnv      = "CHAT_JWT_SECRET"
	inviteCodeEnv     = "CHAT_INVITE_CODE"
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
	cfg := config.Load()
	checks, err := cfg.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	for _, ch := range checks {
		log.Printf("config check ok: %s", ch.Name)
	}

	listenAddr, err := resolveListenAddr(cfg.ListenAddr, os.Getenv(portEnv))
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
	mux.HandleFunc("/ws", wsapi.Handler(h))
	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(h))

	if repository != nil {
		jwtSecret := []byte(os.Getenv(jwtSecretEnv))
		if len(jwtSecret) == 0 {
			log.Fatalf("config: %s must be set when %s is set", jwtSecretEnv, dbPathEnv)
		}
		tickets := auth.NewTicketStore()
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

	// ReadHeaderTimeout caps a slow upgrade handshake (Slowloris). IdleTimeout
	// caps post-upgrade silence on idle keep-alives. WriteTimeout stays zero —
	// it would fight the WebSocket upgrade's hijacked connection.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httpx.BodyCap(mux),
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
