package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hackathon/apps/server/internal/hub"
	"hackathon/apps/server/internal/wsapi"
)

const (
	defaultPort     = "8080"
	portEnv         = "CHAT_SERVER_PORT"
	shutdownTimeout = 5 * time.Second
)

func main() {
	port := os.Getenv(portEnv)
	if port == "" {
		port = defaultPort
	}

	h := hub.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsapi.Handler(h))

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("chat server listening on :%s", port)
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
