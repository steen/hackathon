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

	"hackathon/apps/server/internal/config"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wsapi"
)

const (
	portEnv           = "CHAT_SERVER_PORT"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
)

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

	h := hub.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsapi.Handler(h))
	mux.HandleFunc("/debug/subs", wsapi.DebugSubsHandler(h))

	// ReadHeaderTimeout caps a slow upgrade handshake (Slowloris). IdleTimeout
	// caps post-upgrade silence on idle keep-alives. WriteTimeout stays zero —
	// it would fight the WebSocket upgrade's hijacked connection.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
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
