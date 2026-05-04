package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// DevVersion is the placeholder reported when no module version is
// embedded — i.e. binaries built outside `go install module@vX.Y.Z`
// or a tagged release. Tests pin against this constant.
const DevVersion = "dev"

// Version implements `chatd version` and `chatd --version` / `-v`.
// Output is a single line: `chatd <version> [(<vcs.revision>[ dirty])] <go-version> <GOOS>/<GOARCH>`.
// Example tagged build:
//
//	chatd v1.2.3 (abc1234) go1.22.0 linux/amd64
//
// Example untagged build with VCS info:
//
//	chatd dev (abc1234 dirty) go1.22.0 darwin/arm64
func Version(_ context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return WriteVersion(env.Stdout)
}

// WriteVersion emits the version line so the binary entrypoint can
// call it for `--version` / `-v` without building a full Env.
func WriteVersion(w io.Writer) error {
	_, err := fmt.Fprintln(w, formatVersion(debug.ReadBuildInfo))
	return err
}

// readBuildInfoFn matches debug.ReadBuildInfo so tests can inject a
// fixture without touching the real binary's build metadata.
type readBuildInfoFn func() (*debug.BuildInfo, bool)

func formatVersion(read readBuildInfoFn) string {
	version := DevVersion
	revision := ""
	dirty := false
	if info, ok := read(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	rev := ""
	if revision != "" {
		short := revision
		if len(short) > 7 {
			short = short[:7]
		}
		if dirty {
			rev = fmt.Sprintf(" (%s dirty)", short)
		} else {
			rev = fmt.Sprintf(" (%s)", short)
		}
	}
	return fmt.Sprintf("chatd %s%s %s %s/%s",
		version, rev, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
