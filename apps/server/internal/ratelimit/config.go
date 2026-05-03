package ratelimit

import (
	"os"
	"strconv"
	"time"
)

// Env var names that let test harnesses raise the per-IP register budget
// without changing the production default. PRD §9 keeps Burst=5/15min in
// real deployments; the override exists so the e2e suite can run many
// flows from one IP back-to-back without tripping 429 (issue #114).
const (
	// EnvRegisterBurst overrides RegisterIPConfig().Burst when set to a
	// positive integer.
	EnvRegisterBurst = "CHAT_REGISTER_BURST"
	// EnvRegisterRefill overrides RegisterIPConfig().Refill when set to a
	// duration parseable by time.ParseDuration (e.g. "15m", "30s").
	EnvRegisterRefill = "CHAT_REGISTER_REFILL"
)

// RegisterIPConfigFromEnv returns RegisterIPConfig with optional overrides
// from EnvRegisterBurst and EnvRegisterRefill. Empty, malformed, or
// non-positive values fall back to the production default — callers do not
// need to validate; the override is best-effort and the default holds.
func RegisterIPConfigFromEnv() IPLimiterConfig {
	cfg := RegisterIPConfig()
	if v := os.Getenv(EnvRegisterBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvRegisterRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}
