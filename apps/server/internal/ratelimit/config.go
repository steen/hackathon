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

	// EnvDMWriteBurst overrides DMWriteUserConfig().Burst when set to a
	// positive integer. Phase 9 default is Burst=10 (decision log L17).
	EnvDMWriteBurst = "CHAT_DM_WRITE_BURST"
	// EnvDMWriteRefill overrides DMWriteUserConfig().Refill when set to a
	// duration parseable by time.ParseDuration (e.g. "1m", "30s").
	EnvDMWriteRefill = "CHAT_DM_WRITE_REFILL"

	// EnvReadMarkBurst overrides ReadMarkUserConfig().Burst when set to a
	// positive integer. Phase 9 default is Burst=50 (decision log L17).
	EnvReadMarkBurst = "CHAT_READ_MARK_BURST"
	// EnvReadMarkRefill overrides ReadMarkUserConfig().Refill when set to
	// a duration parseable by time.ParseDuration (e.g. "1m", "30s").
	EnvReadMarkRefill = "CHAT_READ_MARK_REFILL"

	// EnvWrapsNeededBurst overrides WrapsNeededUserConfig().Burst when
	// set to a positive integer. Phase 10 default is Burst=10 (e2e
	// decision log L31 + L36).
	EnvWrapsNeededBurst = "CHAT_WRAPS_NEEDED_BURST"
	// EnvWrapsNeededRefill overrides WrapsNeededUserConfig().Refill when
	// set to a duration parseable by time.ParseDuration (e.g. "1m",
	// "30s").
	EnvWrapsNeededRefill = "CHAT_WRAPS_NEEDED_REFILL"

	// EnvReplayWrapBurst overrides ReplayWrapConfig().Burst when set to
	// a positive integer. Phase 10 default is Burst=3 (e2e decision log
	// L35).
	EnvReplayWrapBurst = "CHAT_REPLAY_WRAP_BURST"
	// EnvReplayWrapRefill overrides ReplayWrapConfig().Refill when set
	// to a duration parseable by time.ParseDuration (e.g. "5m", "30s").
	EnvReplayWrapRefill = "CHAT_REPLAY_WRAP_REFILL"
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

// DMWriteUserConfig is the shared default for the per-user dm-write
// limiter (POST /api/dms/{id}/messages). Burst=10 / Refill=1m mirrors
// the channel-write bucket — DMs and channel messages have the same
// steady-state allowance per the Phase 9 decision log (L17).
//
// The limiter type is IPLimiter — same token-bucket semantics, same
// bounded LRU. The "IP" name is historical; the bucket key is whatever
// string the caller passes (a user ULID here).
func DMWriteUserConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 10, Refill: time.Minute, Capacity: 4096}
}

// DMWriteUserConfigFromEnv returns DMWriteUserConfig with optional
// overrides from EnvDMWriteBurst and EnvDMWriteRefill. Empty,
// malformed, or non-positive values fall back to the production
// default — callers do not need to validate.
func DMWriteUserConfigFromEnv() IPLimiterConfig {
	cfg := DMWriteUserConfig()
	if v := os.Getenv(EnvDMWriteBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvDMWriteRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}

// ReadMarkUserConfig is the shared default for the per-user read-mark
// limiter (POST /api/channels/{id}/read + POST /api/dms/{id}/read).
// Burst=50 absorbs UI-driven debounced bursts when a user scrolls
// through several conversations in quick succession; Refill=1m sets
// steady-state allowance. Phase 9 decision log L17.
//
// The limiter type is IPLimiter — same token-bucket semantics, same
// bounded LRU. The "IP" name is historical; the bucket key is whatever
// string the caller passes (a user ULID here).
func ReadMarkUserConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 50, Refill: time.Minute, Capacity: 4096}
}

// ReadMarkUserConfigFromEnv returns ReadMarkUserConfig with optional
// overrides from EnvReadMarkBurst and EnvReadMarkRefill. Empty,
// malformed, or non-positive values fall back to the production
// default — callers do not need to validate.
func ReadMarkUserConfigFromEnv() IPLimiterConfig {
	cfg := ReadMarkUserConfig()
	if v := os.Getenv(EnvReadMarkBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvReadMarkRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}

// WrapsNeededUserConfig is the shared default for the per-user
// wraps-needed-read limiter (GET /api/channels/{id}/members/wraps-needed).
// Burst=10 / Refill=1m matches the cadence of the lazy-wrap-on-online
// loop: a client queries once per WS-connection lifetime per channel
// with a 60s flap-debounce, so a friend-scale member never trips the
// bucket while a misbehaving client cannot loop the endpoint. E2e
// decision log L31 + L36.
//
// The limiter type is IPLimiter — same token-bucket semantics, same
// bounded LRU. The "IP" name is historical; the bucket key is whatever
// string the caller passes (a user ULID here).
func WrapsNeededUserConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 10, Refill: time.Minute, Capacity: 4096}
}

// WrapsNeededUserConfigFromEnv returns WrapsNeededUserConfig with
// optional overrides from EnvWrapsNeededBurst and EnvWrapsNeededRefill.
// Empty, malformed, or non-positive values fall back to the production
// default — callers do not need to validate.
func WrapsNeededUserConfigFromEnv() IPLimiterConfig {
	cfg := WrapsNeededUserConfig()
	if v := os.Getenv(EnvWrapsNeededBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvWrapsNeededRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}

// ReplayWrapConfig is the shared default for the replay-wrap limiter
// (POST /api/channels/{id}/members/{user_id}/replay-wrap). Burst=3 /
// Refill=5m bounds loop-griefing of one recipient by a malicious
// re-issuer; the bucket is keyed per (channel_id, member_user_id) pair
// (handler builds the key, the config function only pins the bucket
// shape). E2e decision log L35.
//
// The limiter type is IPLimiter — same token-bucket semantics, same
// bounded LRU. The "IP" name is historical; the bucket key is whatever
// string the caller passes.
func ReplayWrapConfig() IPLimiterConfig {
	return IPLimiterConfig{Burst: 3, Refill: 5 * time.Minute, Capacity: 4096}
}

// ReplayWrapConfigFromEnv returns ReplayWrapConfig with optional
// overrides from EnvReplayWrapBurst and EnvReplayWrapRefill. Empty,
// malformed, or non-positive values fall back to the production
// default — callers do not need to validate.
func ReplayWrapConfigFromEnv() IPLimiterConfig {
	cfg := ReplayWrapConfig()
	if v := os.Getenv(EnvReplayWrapBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Burst = n
		}
	}
	if v := os.Getenv(EnvReplayWrapRefill); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Refill = d
		}
	}
	return cfg
}
