// Package logging builds the slog.Logger the server bootstrap installs
// as slog.Default. Scope is intentionally narrow: map a CHAT_LOG_LEVEL
// string (PRD §9) to a slog.Level and return a text-handler logger
// writing to os.Stdout. JSON output is not in scope.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level names accepted by ParseLevel and New. The set matches PRD §9's
// CHAT_LOG_LEVEL values; the corresponding slog.Level is returned by
// ParseLevel. DefaultLevel is the fallback for an unknown name.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"

	DefaultLevel = LevelInfo
)

// ParseLevel maps a PRD §9 level string to its slog.Level. Comparison
// is case-insensitive after trimming whitespace; an unrecognized name
// (including the empty string) returns slog.LevelInfo and ok=false so
// callers can decide whether to warn.
func ParseLevel(name string) (lvl slog.Level, ok bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case LevelDebug:
		return slog.LevelDebug, true
	case LevelInfo:
		return slog.LevelInfo, true
	case LevelWarn:
		return slog.LevelWarn, true
	case LevelError:
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

// New returns a slog.Logger configured with a text handler at the level
// named by `name`. Output goes to os.Stdout. Unknown names fall back to
// info silently — the caller (config.Load) is expected to have already
// surfaced any invalid value.
func New(name string) *slog.Logger {
	return NewWithWriter(name, os.Stdout)
}

// NewWithWriter is New with an explicit destination, used by tests so
// they can capture log output without touching os.Stdout.
func NewWithWriter(name string, w io.Writer) *slog.Logger {
	lvl, _ := ParseLevel(name)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}
