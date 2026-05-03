package ratelimit

import (
	"testing"
	"time"
)

func TestRegisterIPConfigFromEnvDefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvRegisterBurst, "")
	t.Setenv(EnvRegisterRefill, "")

	got := RegisterIPConfigFromEnv()
	want := RegisterIPConfig()
	if got.Burst != want.Burst {
		t.Errorf("Burst: got %d, want %d", got.Burst, want.Burst)
	}
	if got.Refill != want.Refill {
		t.Errorf("Refill: got %v, want %v", got.Refill, want.Refill)
	}
	if got.Capacity != want.Capacity {
		t.Errorf("Capacity: got %d, want %d", got.Capacity, want.Capacity)
	}
}

func TestRegisterIPConfigFromEnvOverridesBurst(t *testing.T) {
	t.Setenv(EnvRegisterBurst, "200")
	t.Setenv(EnvRegisterRefill, "")

	got := RegisterIPConfigFromEnv()
	if got.Burst != 200 {
		t.Errorf("Burst: got %d, want 200", got.Burst)
	}
	if got.Refill != RegisterIPConfig().Refill {
		t.Errorf("Refill: got %v, want default %v", got.Refill, RegisterIPConfig().Refill)
	}
}

func TestRegisterIPConfigFromEnvOverridesRefill(t *testing.T) {
	t.Setenv(EnvRegisterBurst, "")
	t.Setenv(EnvRegisterRefill, "30s")

	got := RegisterIPConfigFromEnv()
	if got.Refill != 30*time.Second {
		t.Errorf("Refill: got %v, want 30s", got.Refill)
	}
	if got.Burst != RegisterIPConfig().Burst {
		t.Errorf("Burst: got %d, want default %d", got.Burst, RegisterIPConfig().Burst)
	}
}

func TestRegisterIPConfigFromEnvIgnoresInvalidBurst(t *testing.T) {
	cases := []string{"abc", "0", "-5", " "}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(EnvRegisterBurst, raw)
			t.Setenv(EnvRegisterRefill, "")
			got := RegisterIPConfigFromEnv()
			if got.Burst != RegisterIPConfig().Burst {
				t.Errorf("invalid %q should not override Burst (got %d)", raw, got.Burst)
			}
		})
	}
}

func TestRegisterIPConfigFromEnvIgnoresInvalidRefill(t *testing.T) {
	cases := []string{"abc", "0s", "-1m", "15"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(EnvRegisterBurst, "")
			t.Setenv(EnvRegisterRefill, raw)
			got := RegisterIPConfigFromEnv()
			if got.Refill != RegisterIPConfig().Refill {
				t.Errorf("invalid %q should not override Refill (got %v)", raw, got.Refill)
			}
		})
	}
}

func TestRegisterIPConfigFromEnvBothOverrides(t *testing.T) {
	t.Setenv(EnvRegisterBurst, "1000")
	t.Setenv(EnvRegisterRefill, "1s")

	got := RegisterIPConfigFromEnv()
	if got.Burst != 1000 {
		t.Errorf("Burst: got %d, want 1000", got.Burst)
	}
	if got.Refill != time.Second {
		t.Errorf("Refill: got %v, want 1s", got.Refill)
	}
	// Production default for Capacity must still apply.
	if got.Capacity != RegisterIPConfig().Capacity {
		t.Errorf("Capacity: got %d, want default %d", got.Capacity, RegisterIPConfig().Capacity)
	}
}
