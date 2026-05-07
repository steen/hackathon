package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"runtime/debug"

	"hackathon/internal/buildinfo"
)

// DevVersion is the placeholder reported when no module version is
// embedded — i.e. binaries built outside `go install module@vX.Y.Z`
// or a tagged release. Tests pin against this constant.
const DevVersion = buildinfo.DevVersion

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
	return buildinfo.ReadWith(buildinfo.ReadBuildInfoFn(read)).FormatLine()
}
