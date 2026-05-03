package cmd

import (
	"net/url"
	"os"
)

const DefaultURL = "ws://localhost:8080/ws"

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
