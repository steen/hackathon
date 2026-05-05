package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in     string
		want   slog.Level
		wantOK bool
	}{
		{"debug", slog.LevelDebug, true},
		{"info", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"DEBUG", slog.LevelDebug, true},
		{"  Info  ", slog.LevelInfo, true},
		{"WARN", slog.LevelWarn, true},
		{"Error", slog.LevelError, true},
		{"", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, false},
		{"trace", slog.LevelInfo, false},
		{"warning", slog.LevelInfo, false},
	}
	for _, tc := range cases {
		got, ok := ParseLevel(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("ParseLevel(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestNewWithWriter_FiltersBelowConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("warn", &buf)

	logger.Debug("d-line")
	logger.Info("i-line")
	logger.Warn("w-line")
	logger.Error("e-line")

	out := buf.String()
	if strings.Contains(out, "d-line") {
		t.Errorf("warn handler emitted debug line: %q", out)
	}
	if strings.Contains(out, "i-line") {
		t.Errorf("warn handler emitted info line: %q", out)
	}
	if !strings.Contains(out, "w-line") {
		t.Errorf("warn handler dropped warn line: %q", out)
	}
	if !strings.Contains(out, "e-line") {
		t.Errorf("warn handler dropped error line: %q", out)
	}
}

func TestNewWithWriter_DebugEmitsEverything(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("debug", &buf)
	logger.Debug("d-line")
	logger.Info("i-line")
	logger.Warn("w-line")
	logger.Error("e-line")
	out := buf.String()
	for _, needle := range []string{"d-line", "i-line", "w-line", "e-line"} {
		if !strings.Contains(out, needle) {
			t.Errorf("debug handler dropped %q: %q", needle, out)
		}
	}
}

func TestNewWithWriter_UnknownNameFallsBackToInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("verbose", &buf)
	logger.Debug("d-line")
	logger.Info("i-line")
	out := buf.String()
	if strings.Contains(out, "d-line") {
		t.Errorf("unknown level should default to info, got debug emitted: %q", out)
	}
	if !strings.Contains(out, "i-line") {
		t.Errorf("unknown level should default to info, got info dropped: %q", out)
	}
}
