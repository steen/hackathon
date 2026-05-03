// Package server contains the HTTP/WebSocket handlers for the chat server.
package server

import (
	"context"
	"net/http"

	"github.com/coder/websocket"

	"github.com/jumoel/hackathon/apps/server/internal/hub"
)

// GeneralChannel is the only channel served in phase 0.
const GeneralChannel = "#general"

const sendBuffer = 16

// WSHandler upgrades incoming HTTP requests on /ws to WebSocket connections
// and registers each connection as a subscriber on GeneralChannel.
type WSHandler struct {
	hub *hub.Hub
}

// NewWSHandler returns a handler that registers connections with h.
func NewWSHandler(h *hub.Hub) *WSHandler {
	return &WSHandler{hub: h}
}

// ServeHTTP performs the WebSocket handshake, registers the connection with
// the hub against GeneralChannel, and serves the connection until it closes.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept already wrote a 4xx response with the failure reason.
		return
	}
	defer c.CloseNow()

	sub := hub.NewSubscriber(sendBuffer)
	h.hub.Subscribe(GeneralChannel, sub)
	defer h.hub.Unsubscribe(GeneralChannel, sub)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-sub.Send:
				if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		h.hub.Broadcast(GeneralChannel, data)
	}
	cancel()
	<-writerDone
}
