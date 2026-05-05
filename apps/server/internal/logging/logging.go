// Package logging builds the slog.Logger the server bootstrap installs
// as slog.Default. Scope is intentionally narrow: map a CHAT_LOG_LEVEL
// string (PRD §9) to a slog.Level and return a text-handler logger that
// routes records below slog.LevelWarn to os.Stdout and warn+ to
// os.Stderr (typical operator convention — see issue #715). JSON output
// is not in scope.
package logging

import (
	"context"
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

// New returns a slog.Logger configured at the level named by `name`.
// Records below slog.LevelWarn are written to os.Stdout; warn and error
// records are written to os.Stderr. Unknown names fall back to info
// silently — the caller (config.Load) is expected to have already
// surfaced any invalid value.
func New(name string) *slog.Logger {
	return NewWithWriters(name, os.Stdout, os.Stderr)
}

// NewWithWriter is New with a single destination for every level. It is
// used by tests that don't care about the stdout/stderr split; both the
// low and high handlers share the same writer.
func NewWithWriter(name string, w io.Writer) *slog.Logger {
	return NewWithWriters(name, w, w)
}

// NewWithWriters is New with explicit low (info-and-below) and high
// (warn+) destinations. Both inner handlers share the same level
// threshold from `name`; the split decides only which stream a record
// lands on, never whether it is emitted.
func NewWithWriters(name string, low, high io.Writer) *slog.Logger {
	lvl, _ := ParseLevel(name)
	opts := &slog.HandlerOptions{Level: lvl}
	return slog.New(&splitHandler{
		low:  slog.NewTextHandler(low, opts),
		high: slog.NewTextHandler(high, opts),
	})
}

// splitHandler routes each record to exactly one of two slog.Handlers
// based on its level: warn and above go to `high`, the rest to `low`.
// WithAttrs and WithGroup propagate to both inner handlers so attached
// context is preserved on whichever stream a later record uses.
type splitHandler struct {
	low, high slog.Handler
}

// Enabled reports whether either inner handler will accept a record at
// lvl; both share the same level threshold so this is effectively the
// shared threshold check.
func (h *splitHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.low.Enabled(ctx, lvl) || h.high.Enabled(ctx, lvl)
}

// Handle dispatches r to the high handler when its level is warn or
// above and to the low handler otherwise. Each record reaches exactly
// one inner handler.
func (h *splitHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		return h.high.Handle(ctx, r)
	}
	return h.low.Handle(ctx, r)
}

// WithAttrs returns a splitHandler whose inner handlers each carry attrs.
func (h *splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &splitHandler{
		low:  h.low.WithAttrs(attrs),
		high: h.high.WithAttrs(attrs),
	}
}

// WithGroup returns a splitHandler whose inner handlers each open the
// named group, so subsequent attrs are nested on whichever stream a
// record lands on.
func (h *splitHandler) WithGroup(name string) slog.Handler {
	return &splitHandler{
		low:  h.low.WithGroup(name),
		high: h.high.WithGroup(name),
	}
}
