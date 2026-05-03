// Command server runs the chat WebSocket server. It listens on the address
// resolved from the environment by internal/config and registers a single
// /ws handler backed by the in-memory hub.
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/jumoel/hackathon/apps/server/internal/config"
	"github.com/jumoel/hackathon/apps/server/internal/hub"
	"github.com/jumoel/hackathon/apps/server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	h := hub.New()
	mux := http.NewServeMux()
	mux.Handle("/ws", server.NewWSHandler(h))

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("listening on %s", cfg.Addr())
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
