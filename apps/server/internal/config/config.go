// Package config resolves the server's runtime configuration from the
// environment. Validation happens here so callers can fail fast at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// EnvPort is the environment variable used to override the listen port.
const EnvPort = "SERVER_PORT"

// DefaultPort is the listen port used when EnvPort is unset.
const DefaultPort = 8080

// Config holds resolved server settings.
type Config struct {
	Port int
}

// Addr returns the TCP listen address (host omitted, all interfaces).
func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// Load reads configuration from the environment, applying defaults and
// validating. It returns an error rather than silently falling back when an
// explicit value is invalid.
func Load() (Config, error) {
	port := DefaultPort
	if raw, ok := os.LookupEnv(EnvPort); ok {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: %s=%q is not a valid integer: %w", EnvPort, raw, err)
		}
		if n < 1 || n > 65535 {
			return Config{}, fmt.Errorf("config: %s=%d is out of range [1,65535]", EnvPort, n)
		}
		port = n
	}
	return Config{Port: port}, nil
}
