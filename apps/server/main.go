package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hackathon/apps/server/internal/config"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wiring"
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
	maxHeaderBytes    = 16 * 1024
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	cfg := config.Load()
	checks, err := cfg.Validate()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	for _, ch := range checks {
		log.Printf("config check ok: %s", ch.Name)
	}
	if cfg.AllowPublicBind && !cfg.TrustedProxy {
		log.Printf("WARN: %s=1 with %s unset; if you are behind a reverse proxy, IP rate limits will key on the proxy IP. Set %s=1 to honor X-Forwarded-For (see PRD §9).", config.EnvAllowPublicBind, config.EnvTrustedProxy, config.EnvTrustedProxy)
	}

	listenAddr, err := resolveListenAddr(cfg.ListenAddr, os.Getenv(portEnv))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	deps := wiring.Deps{
		Hub:            hub.New(),
		AllowedOrigins: parseAllowedOrigins(os.Getenv(allowedOriginsEnv)),
	}
	log.Printf("config check ok: %s parsed %d origin pattern(s)", allowedOriginsEnv, len(deps.AllowedOrigins))

	sqlDB, repository, err := openAndMigrate(os.Getenv(dbPathEnv))
	if err != nil {
		return err
	}
	if sqlDB != nil {
		defer func() { _ = sqlDB.Close() }()
		jwtSecret, err := requireSecret(jwtSecretEnv, dbPathEnv)
		if err != nil {
			return err
		}
		deps.Repo = repository
		deps.JWTSecret = jwtSecret
		deps.InviteCode = os.Getenv(inviteCodeEnv)
		log.Printf("db ready at %q", os.Getenv(dbPathEnv))
	}

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           wiring.Build(deps),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
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
