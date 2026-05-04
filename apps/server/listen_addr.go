package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

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
