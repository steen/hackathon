// Package cmd holds the chatd CLI subcommand entry points. Each
// exported function takes an *Env (stdin/stdout/stderr + config dir +
// resolved server URL) so the binary entrypoint and tests can drive
// commands without touching globals.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"hackathon/apps/cli/internal/config"
	goclient "hackathon/packages/go-client"
)

// DefaultServer is the base URL chatd targets when neither --server nor
// $CHAT_SERVER is set.
const DefaultServer = "http://localhost:8080"

// Env is the per-invocation context every subcommand needs. ConfigDir
// is honoured by the config package so tests can isolate to t.TempDir().
type Env struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	ConfigDir string
	Server    string

	// stdinReader caches a bufio.Reader over Stdin so multi-prompt
	// flows (register's password+invite, login's username+password)
	// don't lose pre-buffered bytes when scripted via heredoc/<<<.
	// Lazy-initialised; readLine is the sole accessor.
	stdinReader *bufio.Reader
}

// DefaultEnv returns an Env wired to the real process streams. The
// caller fills in Server (resolved from flag/env) before dispatch.
func DefaultEnv() *Env {
	return &Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}

// ResolveServer picks the base URL: explicit flag wins, then
// $CHAT_SERVER, then DefaultServer.
func ResolveServer(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("CHAT_SERVER"); v != "" {
		return v
	}
	return DefaultServer
}

func loadConfig(env *Env) (*config.File, error) {
	return config.Load(env.ConfigDir)
}

func saveConfig(env *Env, f *config.File) error {
	if err := config.Save(env.ConfigDir, f); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// newClient builds a goclient.Client. When requireToken is true and the
// stored token is empty, the function returns ErrNotLoggedIn so the
// caller can surface a single, consistent message.
func newClient(env *Env, requireToken bool) (*goclient.Client, *config.File, error) {
	cfg, err := loadConfig(env)
	if err != nil {
		return nil, nil, err
	}
	base := env.Server
	if base == "" {
		base = cfg.Server
	}
	if base == "" {
		base = DefaultServer
	}
	if requireToken && cfg.Token == "" {
		return nil, cfg, ErrNotLoggedIn
	}
	c := goclient.New(base, goclient.WithToken(cfg.Token))
	return c, cfg, nil
}

// ErrNotLoggedIn is returned by commands that require a stored token.
var ErrNotLoggedIn = errors.New("not logged in (run `chatd login` or `chatd register`)")
