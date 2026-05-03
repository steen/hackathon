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
	"syscall"
	"time"

	"hackathon/apps/server/internal/config"
	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wsapi"
)

const (
	defaultPort       = 8080
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

	port, err := resolvePort(os.Getenv(portEnv))
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
