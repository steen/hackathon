package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hackathon/apps/server/internal/config"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/logging"
	"hackathon/apps/server/internal/wiring"
	"hackathon/internal/buildinfo"
)

const (
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 16 * 1024
)

func main() {
	// Probe runs before config.Load() so a misconfigured server can
	// still be probed by docker's HEALTHCHECK (which won't supply the
	// env vars the server validates). See apps/server/probe.go and #796.
	if hasHealthProbeFlag(os.Args[1:]) {
		os.Exit(runHealthProbe())
	}
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

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)
	// slog.SetDefault re-routes log.Default()'s output through the slog
	// handler. The access-log middleware (apps/server/internal/http) still
	// writes via stdlib log.Printf and emits a self-describing line; we
	// don't want it wrapped in slog's `time=… level=INFO msg="…"` envelope.
	// Restore the stdlib log destination so middleware lines stay raw.
	log.SetOutput(os.Stderr)

	slog.LogAttrs(context.Background(), slog.LevelInfo, "server build", buildinfo.Read().LogAttrs()...)

	if cfg.LogLevelInvalid != "" {
		slog.Warn("ignoring unrecognized log level; falling back to default",
			"env", config.EnvLogLevel,
			"got", cfg.LogLevelInvalid,
			"using", cfg.LogLevel,
		)
	}
	if err := applyBcryptCost(cfg.BcryptCost); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	for _, ch := range checks {
		slog.Info("config check ok", "name", ch.Name)
	}
	if cfg.AllowPublicBind && !cfg.TrustedProxy {
		slog.Warn("public bind without trusted-proxy may key rate limits on the proxy IP",
			"env_public_bind", config.EnvAllowPublicBind,
			"env_trusted_proxy", config.EnvTrustedProxy,
			"hint", "set "+config.EnvTrustedProxy+"=1 to honor X-Forwarded-For (PRD §9)",
		)
	}

	listenAddr := cfg.ListenAddr

	deps := wiring.Deps{
		Hub:            hub.New(),
		AllowedOrigins: parseAllowedOrigins(os.Getenv(config.EnvAllowedOrigins)),
	}
	slog.Info("config check ok",
		"name", "allowed_origins_parsed",
		"env", config.EnvAllowedOrigins,
		"count", len(deps.AllowedOrigins),
	)

	// openAndMigrate, requireSecret and other bootstrap helpers live in boot.go.
	sqlDB, repository, err := openAndMigrate(os.Getenv(config.EnvDBPath))
	if err != nil {
		return err
	}
	if sqlDB != nil {
		defer func() { _ = sqlDB.Close() }()
		jwtSecret, err := requireSecret(config.EnvJWTSecret, config.EnvDBPath)
		if err != nil {
			return err
		}
		deps.Repo = repository
		deps.JWTSecret = jwtSecret
		deps.InviteCode = os.Getenv(config.EnvInviteCode)
		slog.Info("db ready", "path", os.Getenv(config.EnvDBPath))
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
		slog.Info("chat server listening", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	deps.Hub.CloseAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err.Error())
	}
	return nil
}
