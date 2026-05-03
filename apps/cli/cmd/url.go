// Package cmd holds the chatd CLI subcommand entry points.
package cmd

import "os"

// DefaultURL is the chatd WebSocket endpoint used when neither --url nor
// CHAT_SERVER is set.
const DefaultURL = "ws://localhost:8080/ws"

// ResolveURL picks the WebSocket URL for chatd, preferring the explicit
// flag, then $CHAT_SERVER, then DefaultURL.
func ResolveURL(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("CHAT_SERVER"); env != "" {
		return env
	}
	return DefaultURL
}
