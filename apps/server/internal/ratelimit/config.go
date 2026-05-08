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

	// EnvChannelWriteBurst overrides ChannelWriteUserConfig().Burst when
	// set to a positive integer. PRD §9 keeps Burst=10 in real deployments.
	EnvChannelWriteBurst = "CHAT_CHANNEL_WRITE_BURST"
	// EnvChannelWriteRefill overrides ChannelWriteUserConfig().Refill when
	// set to a duration parseable by time.ParseDuration (e.g. "1m", "30s").
	EnvChannelWriteRefill = "CHAT_CHANNEL_WRITE_REFILL"
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

// ChannelWriteUserConfig is the shared default for the per-user
// channel-write limiter (POST + PATCH /api/channels). Burst=10 lets a
// fresh user create their initial channel set without friction; Refill=1m
// produces one extra write per minute thereafter. PRD §9.
//
// The limiter type is IPLimiter — same token-bucket semantics, same
// bounded LRU. The "IP" name is historical; the bucket key is whatever
// string the caller passes (a user ULID here).
func ChannelWriteUserConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 10, Refill: time.Minute, Capacity: 4096}
}

// ChannelWriteUserConfigFromEnv returns ChannelWriteUserConfig with
// optional overrides from EnvChannelWriteBurst and EnvChannelWriteRefill.
// Empty, malformed, or non-positive values fall back to the production
// default — callers do not need to validate.
func ChannelWriteUserConfigFromEnv() IPLimiterConfig {
	cfg := ChannelWriteUserConfig()
	if v := os.Getenv(EnvChannelWriteBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvChannelWriteRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}
