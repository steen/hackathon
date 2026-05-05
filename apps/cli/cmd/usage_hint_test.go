package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// usageEnv builds a minimal Env that's enough to drive the usage-error
// branch in History/Send/Register/Watch. None of those branches reach
// the network, so a config dir / fake server isn't required.
func usageEnv(t *testing.T) *Env {
	t.Helper()
	return &Env{
		Stdin:     strings.NewReader(""),
		Stdout:    &bytes.Buffer{},
		Stderr:    io.Discard,
		ConfigDir: t.TempDir(),
	}
}

func TestUsageErrorsIncludeHFlagHint(t *testing.T) {
	cases := []struct {
		name string
		run  func(context.Context, *Env, []string) error
		args []string
		hint string
	}{
		{"history", History, nil, "run 'chatd history -h' for full flags"},
		{"send", Send, nil, "run 'chatd send -h' for full flags"},
		{"register", Register, nil, "run 'chatd register -h' for full flags"},
		{"watch", Watch, nil, "run 'chatd watch -h' for full flags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(context.Background(), usageEnv(t), tc.args)
			if err == nil {
				t.Fatalf("%s: expected usage error, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "usage: chatd "+tc.name) {
				t.Errorf("%s: err = %q, want usage prefix", tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("%s: err = %q, want hint %q", tc.name, err, tc.hint)
			}
		})
	}
}
