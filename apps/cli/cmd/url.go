// Package cmd implements the chatd CLI subcommands.
package cmd

import (
	"fmt"
	"strings"

	"github.com/jumoel/hackathon/packages/go-shared/serverdefaults"
)

// EnvServerURL is the environment variable that overrides the default dial URL.
const EnvServerURL = "CHAT_SERVER"

// DefaultURL is the WebSocket URL the CLI dials when no flag or env var is set.
var DefaultURL = fmt.Sprintf("ws://localhost:%d/ws", serverdefaults.Port)

// ResolveURL picks the WebSocket URL to dial.
//
// Precedence: explicit flag > CHAT_SERVER env var > localhost default.
// Whitespace-only values are treated as unset so callers don't dial a
// malformed URL.
func ResolveURL(flag, env string) string {
	if v := strings.TrimSpace(flag); v != "" {
		return v
	}
	if v := strings.TrimSpace(env); v != "" {
		return v
	}
	return DefaultURL
}
