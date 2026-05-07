// Package buildinfo extracts build-time identity (version, VCS revision,
// dirty flag, Go toolchain, OS/arch) from runtime/debug.ReadBuildInfo so
// both apps/cli and apps/server render the same shape.
//
// The CLI uses FormatLine for its `chatd version` output. The server uses
// LogAttrs to emit a structured slog record on startup.
package buildinfo

import (
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
)

// DevVersion is the placeholder reported when no module version is
// embedded — i.e. binaries built outside `go install module@vX.Y.Z` or
// a tagged release.
const DevVersion = "dev"

// Info captures the identifying fields of a build.
type Info struct {
	Version  string
	Revision string
	Go       string
	OS       string
	Arch     string
	Dirty    bool
}

// ReadBuildInfoFn matches debug.ReadBuildInfo so callers can inject a
// fixture in tests without touching the real binary's build metadata.
type ReadBuildInfoFn func() (*debug.BuildInfo, bool)

// Read returns the running binary's Info using runtime/debug.
func Read() Info {
	return ReadWith(debug.ReadBuildInfo)
}

// ReadWith returns Info from the supplied reader. The reader has the
// same signature as debug.ReadBuildInfo; a fixture-returning function
// keeps tests deterministic across build environments.
func ReadWith(read ReadBuildInfoFn) Info {
	info := Info{
		Version: DevVersion,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	bi, ok := read()
	if !ok {
		return info
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.modified":
			info.Dirty = s.Value == "true"
		}
	}
	return info
}

// FormatLine renders a single human-readable line in the shape used by
// `chatd version`, e.g. `chatd v1.2.3 (abc1234) go1.22.0 linux/amd64`
// or `chatd dev (abc1234 dirty) go1.22.0 darwin/arm64`. When no VCS
// revision is embedded, the parenthesised section is omitted.
func (i Info) FormatLine() string {
	rev := ""
	if i.Revision != "" {
		short := i.Revision
		if len(short) > 7 {
			short = short[:7]
		}
		if i.Dirty {
			rev = fmt.Sprintf(" (%s dirty)", short)
		} else {
			rev = fmt.Sprintf(" (%s)", short)
		}
	}
	return fmt.Sprintf("chatd %s%s %s %s/%s",
		i.Version, rev, i.Go, i.OS, i.Arch)
}

// LogAttrs returns the Info as slog.Attr values suitable for
// slog.LogAttrs. The keys are stable: version, revision, dirty, go, os,
// arch.
func (i Info) LogAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String("version", i.Version),
		slog.String("revision", i.Revision),
		slog.Bool("dirty", i.Dirty),
		slog.String("go", i.Go),
		slog.String("os", i.OS),
		slog.String("arch", i.Arch),
	}
}
