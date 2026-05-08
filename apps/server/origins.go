package main

import "strings"

// parseAllowedOrigins splits a comma-separated CHAT_ALLOWED_ORIGINS
// value into the OriginPatterns shape coder/websocket expects. Empty
// or whitespace-only entries are dropped so a stray trailing comma is
// not treated as a wildcard.
func parseAllowedOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
