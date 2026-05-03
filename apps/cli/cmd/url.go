// Package cmd holds the chatd CLI subcommand entry points.
package cmd

import (
	"net/url"
	"os"
)

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

// AppendTicket returns rawURL with a `ticket=<ticket>` query parameter
// added. An empty ticket returns rawURL unchanged so callers can pass
// the result of an optional flag without branching. Existing query
// parameters are preserved.
func AppendTicket(rawURL, ticket string) string {
	if ticket == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set("ticket", ticket)
	u.RawQuery = q.Encode()
	return u.String()
}
