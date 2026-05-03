package cmd

import (
	"flag"
	"reflect"
	"strings"
	"testing"
)

// TestSplitFlagsAndPositional pins the parser-level invariants the
// History impl depends on: flags after the positional arg are pulled
// out into the flag tail, --foo=bar stays a single token, -- ends
// flag scanning, and unknown flags pass through to fs.Parse for the
// canonical error.
func TestSplitFlagsAndPositional(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantFlags  []string
		wantPosArg []string
	}{
		{
			name:       "flag-after-positional separate value",
			args:       []string{"chan-1", "--limit", "2"},
			wantFlags:  []string{"--limit", "2"},
			wantPosArg: []string{"chan-1"},
		},
		{
			name:       "flag-after-positional equals form",
			args:       []string{"chan-1", "--limit=3"},
			wantFlags:  []string{"--limit=3"},
			wantPosArg: []string{"chan-1"},
		},
		{
			name:       "flag-before-positional",
			args:       []string{"--limit", "5", "chan-1"},
			wantFlags:  []string{"--limit", "5"},
			wantPosArg: []string{"chan-1"},
		},
		{
			name:       "interleaved",
			args:       []string{"--limit", "5", "chan-1", "--before", "m-9"},
			wantFlags:  []string{"--limit", "5", "--before", "m-9"},
			wantPosArg: []string{"chan-1"},
		},
		{
			name:       "double-dash terminator",
			args:       []string{"--limit", "5", "--", "--literal-channel"},
			wantFlags:  []string{"--limit", "5"},
			wantPosArg: []string{"--literal-channel"},
		},
		{
			name:       "single-dash flag separate value",
			args:       []string{"chan-1", "-limit", "7"},
			wantFlags:  []string{"-limit", "7"},
			wantPosArg: []string{"chan-1"},
		},
		{
			name:       "no flags",
			args:       []string{"chan-1"},
			wantFlags:  nil,
			wantPosArg: []string{"chan-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("history", flag.ContinueOnError)
			_ = fs.Int("limit", 0, "")
			_ = fs.String("before", "", "")
			gotFlags, gotPos := splitFlagsAndPositional(fs, tc.args)
			if !reflect.DeepEqual(gotFlags, tc.wantFlags) {
				t.Errorf("flagArgs = %#v, want %#v", gotFlags, tc.wantFlags)
			}
			if !reflect.DeepEqual(gotPos, tc.wantPosArg) {
				t.Errorf("positional = %#v, want %#v", gotPos, tc.wantPosArg)
			}
		})
	}
}

// TestSplitFlagsAndPositional_BoolFlag covers the bool-flag branch:
// a bool flag must NOT consume the next token even when no `=` is
// present, otherwise `--verbose chan-1` would eat the channel.
func TestSplitFlagsAndPositional_BoolFlag(t *testing.T) {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	_ = fs.Bool("verbose", false, "")
	gotFlags, gotPos := splitFlagsAndPositional(fs, []string{"--verbose", "chan-1"})
	if !reflect.DeepEqual(gotFlags, []string{"--verbose"}) {
		t.Errorf("flagArgs = %#v, want [--verbose]", gotFlags)
	}
	if !reflect.DeepEqual(gotPos, []string{"chan-1"}) {
		t.Errorf("positional = %#v, want [chan-1]", gotPos)
	}
}

// TestHistoryAcceptsFlagsAfterPositional drives History end-to-end
// against the in-package fake server with the AC-documented call
// shape. Without the splitter, fs.Parse would leave --limit in
// fs.Args() and History would error with the usage string. The fake
// server ignores --limit/--before semantically (those query params
// are honoured by the real server and covered by the e2e suite); this
// test only pins that the CLI does not reject the ordering.
func TestHistoryAcceptsFlagsAfterPositional(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	for _, body := range []string{"hello", "world"} {
		ctx, cancel := mustCtx()
		rig.stdout.Reset()
		if err := Send(ctx, rig.env, []string{channel, body}); err != nil {
			cancel()
			t.Fatalf("Send %q: %v", body, err)
		}
		cancel()
	}

	rig.stdout.Reset()
	ctx, cancel := mustCtx()
	defer cancel()
	if err := History(ctx, rig.env, []string{channel, "--limit", "2"}); err != nil {
		t.Fatalf("History flags-after-positional: %v", err)
	}
	if got := rig.stdout.String(); !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("history output missing messages; got:\n%s", got)
	}
}

func TestHistoryAcceptsBeforeFlagAfterPositional(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	if err := History(ctx, rig.env, []string{channel, "--before", "01CURSORAAAAAAAAAAAAAAAAAA"}); err != nil {
		t.Fatalf("History --before after positional: %v", err)
	}
}

func TestHistoryAcceptsEqualsFormFlagAfterPositional(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	if err := History(ctx, rig.env, []string{channel, "--limit=1"}); err != nil {
		t.Fatalf("History --limit=1 after positional: %v", err)
	}
}

func TestHistoryStillAcceptsFlagsBeforePositional(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	if err := History(ctx, rig.env, []string{"--limit", "2", channel}); err != nil {
		t.Fatalf("History flags-before-positional: %v", err)
	}
}

func TestHistoryRejectsExtraPositional(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	err := History(ctx, rig.env, []string{channel, "extra", "--limit", "2"})
	if err == nil {
		t.Fatal("History with two positionals returned nil; want usage error")
	}
	if !strings.Contains(err.Error(), "usage: chatd history") {
		t.Errorf("err = %v, want usage error", err)
	}
}

func TestHistoryRejectsUnknownFlag(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	err := History(ctx, rig.env, []string{channel, "--bogus", "x"})
	if err == nil {
		t.Fatal("History with unknown flag returned nil; want flag.Parse error")
	}
}
