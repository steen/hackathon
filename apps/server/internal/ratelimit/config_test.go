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

func TestChannelWriteUserConfigDefaults(t *testing.T) {
	got := ChannelWriteUserConfig()
	if got.Burst != 10 {
		t.Errorf("Burst: got %d, want 10 (PRD §9 default)", got.Burst)
	}
	if got.Refill != time.Minute {
		t.Errorf("Refill: got %v, want 1m (PRD §9 default)", got.Refill)
	}
}

func TestChannelWriteUserConfigFromEnvDefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvChannelWriteBurst, "")
	t.Setenv(EnvChannelWriteRefill, "")
	got := ChannelWriteUserConfigFromEnv()
	want := ChannelWriteUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestChannelWriteUserConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(EnvChannelWriteBurst, "3")
	t.Setenv(EnvChannelWriteRefill, "5s")
	got := ChannelWriteUserConfigFromEnv()
	if got.Burst != 3 {
		t.Errorf("Burst: got %d want 3", got.Burst)
	}
	if got.Refill != 5*time.Second {
		t.Errorf("Refill: got %v want 5s", got.Refill)
	}
}

func TestChannelWriteUserConfigFromEnvIgnoresInvalid(t *testing.T) {
	t.Setenv(EnvChannelWriteBurst, "abc")
	t.Setenv(EnvChannelWriteRefill, "0s")
	got := ChannelWriteUserConfigFromEnv()
	want := ChannelWriteUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("invalid envs should not override; got %+v want %+v", got, want)
	}
}

func TestDMWriteUserConfigDefaults(t *testing.T) {
	got := DMWriteUserConfig()
	if got.Burst != 10 {
		t.Errorf("Burst: got %d, want 10 (decision log L17 default)", got.Burst)
	}
	if got.Refill != time.Minute {
		t.Errorf("Refill: got %v, want 1m (decision log L17 default)", got.Refill)
	}
}

func TestDMWriteUserConfigFromEnvDefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvDMWriteBurst, "")
	t.Setenv(EnvDMWriteRefill, "")
	got := DMWriteUserConfigFromEnv()
	want := DMWriteUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestDMWriteUserConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(EnvDMWriteBurst, "3")
	t.Setenv(EnvDMWriteRefill, "5s")
	got := DMWriteUserConfigFromEnv()
	if got.Burst != 3 {
		t.Errorf("Burst: got %d want 3", got.Burst)
	}
	if got.Refill != 5*time.Second {
		t.Errorf("Refill: got %v want 5s", got.Refill)
	}
}

func TestDMWriteUserConfigFromEnvIgnoresInvalid(t *testing.T) {
	t.Setenv(EnvDMWriteBurst, "abc")
	t.Setenv(EnvDMWriteRefill, "0s")
	got := DMWriteUserConfigFromEnv()
	want := DMWriteUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("invalid envs should not override; got %+v want %+v", got, want)
	}
}

func TestReadMarkUserConfigDefaults(t *testing.T) {
	got := ReadMarkUserConfig()
	if got.Burst != 50 {
		t.Errorf("Burst: got %d, want 50 (decision log L17 default)", got.Burst)
	}
	if got.Refill != time.Minute {
		t.Errorf("Refill: got %v, want 1m (decision log L17 default)", got.Refill)
	}
}

func TestReadMarkUserConfigFromEnvDefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvReadMarkBurst, "")
	t.Setenv(EnvReadMarkRefill, "")
	got := ReadMarkUserConfigFromEnv()
	want := ReadMarkUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestReadMarkUserConfigFromEnvOverrides(t *testing.T) {
	t.Setenv(EnvReadMarkBurst, "7")
	t.Setenv(EnvReadMarkRefill, "5s")
	got := ReadMarkUserConfigFromEnv()
	if got.Burst != 7 {
		t.Errorf("Burst: got %d want 7", got.Burst)
	}
	if got.Refill != 5*time.Second {
		t.Errorf("Refill: got %v want 5s", got.Refill)
	}
}

func TestReadMarkUserConfigFromEnvIgnoresInvalid(t *testing.T) {
	t.Setenv(EnvReadMarkBurst, "abc")
	t.Setenv(EnvReadMarkRefill, "0s")
	got := ReadMarkUserConfigFromEnv()
	want := ReadMarkUserConfig()
	if got.Burst != want.Burst || got.Refill != want.Refill {
		t.Errorf("invalid envs should not override; got %+v want %+v", got, want)
	}
}
