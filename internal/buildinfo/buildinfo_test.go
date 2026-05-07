package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// fakeRead returns a ReadBuildInfoFn that yields the supplied
// *debug.BuildInfo and ok value, so each test row controls the input
// without depending on the test binary's actual build metadata.
func fakeRead(bi *debug.BuildInfo, ok bool) ReadBuildInfoFn {
	return func() (*debug.BuildInfo, bool) { return bi, ok }
}

func TestReadWith(t *testing.T) {
	wantGo := runtime.Version()
	wantOS := runtime.GOOS
	wantArch := runtime.GOARCH

	tests := []struct {
		name string
		read ReadBuildInfoFn
		want Info
	}{
		{
			name: "tagged version with clean revision",
			read: fakeRead(&debug.BuildInfo{
				Main: debug.Module{Version: "v1.2.3"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc1234567890"},
					{Key: "vcs.modified", Value: "false"},
				},
			}, true),
			want: Info{
				Version:  "v1.2.3",
				Revision: "abc1234567890",
				Dirty:    false,
				Go:       wantGo,
				OS:       wantOS,
				Arch:     wantArch,
			},
		},
		{
			name: "untagged dirty",
			read: fakeRead(&debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "deadbeefcafe"},
					{Key: "vcs.modified", Value: "true"},
				},
			}, true),
			want: Info{
				Version:  DevVersion,
				Revision: "deadbeefcafe",
				Dirty:    true,
				Go:       wantGo,
				OS:       wantOS,
				Arch:     wantArch,
			},
		},
		{
			name: "untagged clean",
			read: fakeRead(&debug.BuildInfo{
				Main: debug.Module{Version: ""},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "feedface0001"},
					{Key: "vcs.modified", Value: "false"},
				},
			}, true),
			want: Info{
				Version:  DevVersion,
				Revision: "feedface0001",
				Dirty:    false,
				Go:       wantGo,
				OS:       wantOS,
				Arch:     wantArch,
			},
		},
		{
			name: "no vcs info embedded",
			read: fakeRead(&debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
			}, true),
			want: Info{
				Version:  DevVersion,
				Revision: "",
				Dirty:    false,
				Go:       wantGo,
				OS:       wantOS,
				Arch:     wantArch,
			},
		},
		{
			name: "ReadBuildInfo returns ok=false",
			read: fakeRead(nil, false),
			want: Info{
				Version:  DevVersion,
				Revision: "",
				Dirty:    false,
				Go:       wantGo,
				OS:       wantOS,
				Arch:     wantArch,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadWith(tt.read)
			if got != tt.want {
				t.Fatalf("ReadWith() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatLine(t *testing.T) {
	tests := []struct {
		name string
		in   Info
		want string
	}{
		{
			name: "tagged with short revision",
			in: Info{
				Version:  "v1.2.3",
				Revision: "abc1234567890",
				Go:       "go1.22.0",
				OS:       "linux",
				Arch:     "amd64",
			},
			want: "chatd v1.2.3 (abc1234) go1.22.0 linux/amd64",
		},
		{
			name: "dev with dirty revision",
			in: Info{
				Version:  DevVersion,
				Revision: "abc1234567890",
				Dirty:    true,
				Go:       "go1.22.0",
				OS:       "darwin",
				Arch:     "arm64",
			},
			want: "chatd dev (abc1234 dirty) go1.22.0 darwin/arm64",
		},
		{
			name: "no revision omits parens",
			in: Info{
				Version: DevVersion,
				Go:      "go1.22.0",
				OS:      "linux",
				Arch:    "amd64",
			},
			want: "chatd dev go1.22.0 linux/amd64",
		},
		{
			name: "short-revision (<=7) is not truncated further",
			in: Info{
				Version:  "v0.1.0",
				Revision: "abc12",
				Go:       "go1.22.0",
				OS:       "linux",
				Arch:     "amd64",
			},
			want: "chatd v0.1.0 (abc12) go1.22.0 linux/amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.FormatLine()
			if got != tt.want {
				t.Fatalf("FormatLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLogAttrs(t *testing.T) {
	in := Info{
		Version:  "v1.2.3",
		Revision: "abc1234",
		Dirty:    true,
		Go:       "go1.22.0",
		OS:       "linux",
		Arch:     "amd64",
	}
	attrs := in.LogAttrs()

	want := map[string]string{
		"version":  "v1.2.3",
		"revision": "abc1234",
		"dirty":    "true",
		"go":       "go1.22.0",
		"os":       "linux",
		"arch":     "amd64",
	}
	if len(attrs) != len(want) {
		t.Fatalf("LogAttrs() returned %d attrs, want %d", len(attrs), len(want))
	}
	for _, a := range attrs {
		expect, ok := want[a.Key]
		if !ok {
			t.Errorf("unexpected key %q", a.Key)
			continue
		}
		got := a.Value.String()
		if got != expect {
			t.Errorf("attr %q = %q, want %q", a.Key, got, expect)
		}
	}
}

// TestRead_DoesNotPanic exercises the production reader against the
// real test binary's build info; we don't assert specific values
// (those vary across `go test` invocations) but the function must
// produce a usable Info that includes runtime fields.
func TestRead_DoesNotPanic(t *testing.T) {
	got := Read()
	if got.Go == "" || got.OS == "" || got.Arch == "" {
		t.Fatalf("Read() returned incomplete runtime fields: %+v", got)
	}
	// Sanity-check FormatLine renders something non-empty starting
	// with the binary name token.
	line := got.FormatLine()
	if !strings.HasPrefix(line, "chatd ") {
		t.Fatalf("FormatLine() prefix = %q, want chatd ...", line)
	}
}
