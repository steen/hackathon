package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jumoel/hackathon/apps/server/internal/config"
)

func TestAC4_Config_PortFromEnvVar(t *testing.T) {
	t.Run("valid port is used", func(t *testing.T) {
		t.Setenv(config.EnvPort, "8081")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned error %v, want nil", err)
		}
		if cfg.Port != 8081 {
			t.Fatalf("cfg.Port = %d, want 8081", cfg.Port)
		}
		if got, want := cfg.Addr(), ":8081"; !strings.HasSuffix(got, want) {
			t.Fatalf("cfg.Addr() = %q, want suffix %q", got, want)
		}
	})

	t.Run("non-numeric port returns error", func(t *testing.T) {
		t.Setenv(config.EnvPort, "not-a-port")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() with non-numeric port returned nil error, want error")
		}
	})

	t.Run("out-of-range port returns error", func(t *testing.T) {
		t.Setenv(config.EnvPort, "70000")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() with out-of-range port returned nil error, want error")
		}
	})

	t.Run("zero or negative port returns error", func(t *testing.T) {
		t.Setenv(config.EnvPort, "0")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() with port 0 returned nil error, want error")
		}
		t.Setenv(config.EnvPort, "-1")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() with negative port returned nil error, want error")
		}
	})
}

func TestAC4_Config_DefaultPortWhenEnvUnset(t *testing.T) {
	if err := os.Unsetenv(config.EnvPort); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() with %s unset returned error %v, want nil", config.EnvPort, err)
	}
	if cfg.Port != config.DefaultPort {
		t.Fatalf("cfg.Port = %d, want DefaultPort %d", cfg.Port, config.DefaultPort)
	}
}
