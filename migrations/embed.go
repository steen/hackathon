// Package migrations embeds the SQL migration files at compile time so the
// server binary is self-contained — no runtime dependency on the on-disk
// `migrations/` directory. The package lives under `migrations/` so the
// `go:embed` directive can reach the sibling .sql files; `go:embed` cannot
// escape its own package directory.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed *.sql
var files embed.FS

// FS exposes the embedded migration set rooted so that file names ("0001_init.sql")
// appear at the FS root.
var FS fs.FS = files

func init() {
	// Sanity check at startup: the embed directive is silent if the glob
	// matches zero files. Catch that here rather than let an empty migration
	// set silently leave the schema empty.
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		panic(fmt.Errorf("migrations: read embedded fs: %w", err))
	}
	if len(entries) == 0 {
		panic("migrations: embedded migration set is empty")
	}
}
