// Package config persists the chatd CLI's bearer token and server URL.
//
// The file lives at $XDG_CONFIG_HOME/chatd/config.json (falling back to
// ~/.config/chatd/config.json). Permissions are forced to 0600 so a
// shared-host user cannot read another user's token. The token is
// never logged.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File is the JSON shape persisted to disk. Server is the base URL the
// last login/register targeted; commands pick it up so the user does
// not have to repeat --server.
type File struct {
	Server string `json:"server,omitempty"`
	Token  string `json:"token,omitempty"`
	User   *User  `json:"user,omitempty"`
}

// User is the cached identity from the last successful auth call.
// Mirrors goclient.User but kept local so config does not import the
// client package.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Dir returns the directory the config file lives in. Override is for
// tests; when "", the function consults $CHATD_CONFIG_DIR (escape hatch
// for users who want a custom path), then $XDG_CONFIG_HOME/chatd, then
// $HOME/.config/chatd.
func Dir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if v := os.Getenv("CHATD_CONFIG_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "chatd"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".config", "chatd"), nil
}

// Path returns the full path to config.json under Dir(override).
func Path(override string) (string, error) {
	d, err := Dir(override)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the config file. A missing file is not an error — it
// returns an empty File so callers can treat first-run as "not logged
// in". Permission/parse failures are returned as-is.
func Load(override string) (*File, error) {
	p, err := Path(override)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p) //nolint:gosec // path is derived from XDG/HOME; user-controlled scope is intentional
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &File{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &f, nil
}

// Save atomically writes f to the config path with 0600 permissions.
// The temp-file + rename keeps the file from existing in a half-written
// state if the process is killed mid-write.
func Save(override string, f *File) error {
	dir, err := Dir(override)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	final := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, final, err)
	}
	return nil
}

// Clear removes the config file. A missing file is not an error.
func Clear(override string) error {
	p, err := Path(override)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}
