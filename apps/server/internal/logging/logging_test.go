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

func TestNewWithWriters_RoutesByLevel(t *testing.T) {
	cases := []struct {
		name        string
		level       string
		emit        func(*slog.Logger)
		wantLow     []string
		wantNotLow  []string
		wantHigh    []string
		wantNotHigh []string
	}{
		{
			name:  "info_level_routes_info_low_warn_error_high",
			level: "info",
			emit: func(l *slog.Logger) {
				l.Debug("d-line")
				l.Info("i-line")
				l.Warn("w-line")
				l.Error("e-line")
			},
			wantLow:     []string{"i-line"},
			wantNotLow:  []string{"d-line", "w-line", "e-line"},
			wantHigh:    []string{"w-line", "e-line"},
			wantNotHigh: []string{"d-line", "i-line"},
		},
		{
			name:  "debug_level_routes_debug_info_low_warn_error_high",
			level: "debug",
			emit: func(l *slog.Logger) {
				l.Debug("d-line")
				l.Info("i-line")
				l.Warn("w-line")
				l.Error("e-line")
			},
			wantLow:     []string{"d-line", "i-line"},
			wantNotLow:  []string{"w-line", "e-line"},
			wantHigh:    []string{"w-line", "e-line"},
			wantNotHigh: []string{"d-line", "i-line"},
		},
		{
			name:  "info_level_drops_debug_from_both_streams",
			level: "info",
			emit: func(l *slog.Logger) {
				l.Debug("d-line")
			},
			wantNotLow:  []string{"d-line"},
			wantNotHigh: []string{"d-line"},
		},
		{
			name:  "warn_level_drops_info_from_both_streams",
			level: "warn",
			emit: func(l *slog.Logger) {
				l.Info("i-line")
				l.Warn("w-line")
			},
			wantNotLow:  []string{"i-line"},
			wantHigh:    []string{"w-line"},
			wantNotHigh: []string{"i-line"},
		},
		{
			name:  "error_level_routes_only_error_to_high",
			level: "error",
			emit: func(l *slog.Logger) {
				l.Warn("w-line")
				l.Error("e-line")
			},
			wantNotLow:  []string{"w-line", "e-line"},
			wantHigh:    []string{"e-line"},
			wantNotHigh: []string{"w-line"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lo, hi bytes.Buffer
			logger := NewWithWriters(tc.level, &lo, &hi)
			tc.emit(logger)

			lowOut, highOut := lo.String(), hi.String()
			for _, needle := range tc.wantLow {
				if !strings.Contains(lowOut, needle) {
					t.Errorf("low (stdout) missing %q: %q", needle, lowOut)
				}
			}
			for _, needle := range tc.wantNotLow {
				if strings.Contains(lowOut, needle) {
					t.Errorf("low (stdout) unexpectedly contained %q: %q", needle, lowOut)
				}
			}
			for _, needle := range tc.wantHigh {
				if !strings.Contains(highOut, needle) {
					t.Errorf("high (stderr) missing %q: %q", needle, highOut)
				}
			}
			for _, needle := range tc.wantNotHigh {
				if strings.Contains(highOut, needle) {
					t.Errorf("high (stderr) unexpectedly contained %q: %q", needle, highOut)
				}
			}
		})
	}
}

func TestNewWithWriters_WithAttrsPropagatesToBothStreams(t *testing.T) {
	var lo, hi bytes.Buffer
	logger := NewWithWriters("info", &lo, &hi).With("request_id", "abc-123")

	logger.Info("i-line")
	logger.Error("e-line")

	lowOut, highOut := lo.String(), hi.String()
	if !strings.Contains(lowOut, "request_id=abc-123") || !strings.Contains(lowOut, "i-line") {
		t.Errorf("low stream missing attr or message: %q", lowOut)
	}
	if !strings.Contains(highOut, "request_id=abc-123") || !strings.Contains(highOut, "e-line") {
		t.Errorf("high stream missing attr or message: %q", highOut)
	}
}

func TestNewWithWriters_WithGroupPropagatesToBothStreams(t *testing.T) {
	var lo, hi bytes.Buffer
	logger := NewWithWriters("info", &lo, &hi).WithGroup("req")

	logger.Info("i-line", "id", "abc")
	logger.Error("e-line", "id", "abc")

	lowOut, highOut := lo.String(), hi.String()
	if !strings.Contains(lowOut, "req.id=abc") {
		t.Errorf("low stream missing grouped attr: %q", lowOut)
	}
	if !strings.Contains(highOut, "req.id=abc") {
		t.Errorf("high stream missing grouped attr: %q", highOut)
	}
}
