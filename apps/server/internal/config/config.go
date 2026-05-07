// Package config loads and validates server configuration from the
// process environment. Validate() runs once at startup; failures must
// abort the process before any network or DB resource is opened.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"hackathon/apps/server/internal/auth"
)

// Env var names read at startup. Names, not values — gosec's G101
// "hardcoded credentials" check trips on the literal "SECRET" substring.
const (
	EnvJWTSecret       = "CHAT_JWT_SECRET" //nolint:gosec // G101 false positive: env var name, not a credential.
	EnvInviteCode      = "CHAT_INVITE_CODE"
	EnvListenAddr      = "CHAT_LISTEN_ADDR"
	EnvAllowPublicBind = "CHAT_ALLOW_PUBLIC_BIND"
	// EnvTrustedProxy names the deferred PRD §9 trusted-proxy parser env
	// var. Anchored as a constant so the startup warn in main.go and any
	// future parser implementation refer to the same name.
	EnvTrustedProxy = "CHAT_TRUSTED_PROXY"
	EnvLogLevel     = "CHAT_LOG_LEVEL"
	// EnvBcryptCost names the optional bcrypt cost override (PRD §9). When
	// unset or empty, the auth package keeps the default (auth.DefaultBcryptCost,
	// = 10, OWASP floor). When set, it must parse as an integer in
	// [auth.MinBcryptCost, auth.MaxBcryptCost]; ParseBcryptCost errors out
	// of range and the boot path aborts before the listener starts.
	EnvBcryptCost = "CHAT_BCRYPT_COST"

	DefaultListenAddr = "127.0.0.1:8080"
	DefaultLogLevel   = "info"

	MinJWTSecretBytes = 32
)

// validLogLevels is the closed set PRD §9 lists for CHAT_LOG_LEVEL.
// Anything outside this set falls back to DefaultLogLevel at Load time
// (no error — startup must not fail on a typo in a soft setting).
var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

// jwtSecretDenylist holds the obvious dev defaults the PRD §9 starter set
// forbids. Stored lower-cased; comparison is case-insensitive so
// "Change-Me", "SECRET", etc. are also rejected.
var jwtSecretDenylist = []string{
	"change-me",
	"changeme",
	"change_me",
	"secret",
	"dev",
	"development",
	"test",
	"password",
	"placeholder",
	"todo",
	"example",
	"jwt",
	"jwtsecret",
	"jwt-secret",
	"hackathon",
	"discord-lite",
}

// Config is the validated process configuration. Fields are populated
// by Load and checked by Validate. The zero value is not usable.
type Config struct {
	JWTSecret       string
	InviteCode      string
	ListenAddr      string
	AllowPublicBind bool
	// TrustedProxy enables honoring the leftmost X-Forwarded-For entry
	// when extracting the source IP for the access log, the per-IP
	// rate-limit key, and auth-event audit rows. PRD §9 / §11 documents
	// only the binary on/off form (CIDR allowlists are a future
	// enhancement, NOT in scope). True iff CHAT_TRUSTED_PROXY=1; any
	// other value (unset, "0", "true", "yes") leaves the safe default
	// in place: ignore X-Forwarded-For and trust only RemoteAddr.
	TrustedProxy bool
	// LogLevel is the parsed CHAT_LOG_LEVEL (PRD §9): one of
	// "debug" | "info" | "warn" | "error". Load() forces unrecognized
	// values back to DefaultLogLevel so the field is always one of
	// the four canonical strings.
	LogLevel string
	// LogLevelInvalid records the raw env value when Load() rejected it
	// and fell back to DefaultLogLevel. Empty when the env var was unset
	// or already valid. Bootstrap reads this to emit a single warn line
	// naming the bad value before slog.Default is replaced.
	LogLevelInvalid string
}

// Load reads configuration from the environment. It applies defaults
// but does not validate; call Validate() before use.
func Load() Config {
	addr := os.Getenv(EnvListenAddr)
	if addr == "" {
		addr = DefaultListenAddr
	}
	level, invalid := parseLogLevel(os.Getenv(EnvLogLevel))
	return Config{
		JWTSecret:       os.Getenv(EnvJWTSecret),
		InviteCode:      os.Getenv(EnvInviteCode),
		ListenAddr:      addr,
		AllowPublicBind: os.Getenv(EnvAllowPublicBind) == "1",
		TrustedProxy:    os.Getenv(EnvTrustedProxy) == "1",
		LogLevel:        level,
		LogLevelInvalid: invalid,
	}
}

// parseLogLevel normalizes the raw CHAT_LOG_LEVEL env value. Empty or
// whitespace-only input returns DefaultLogLevel with no invalid mark.
// Unrecognized values (e.g. "verbose", "trace") return DefaultLogLevel
// AND surface the raw input via the second return so bootstrap can warn
// once. Comparison is case-insensitive; canonical lower-case is returned.
func parseLogLevel(raw string) (level, invalid string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultLogLevel, ""
	}
	lower := strings.ToLower(trimmed)
	if _, ok := validLogLevels[lower]; ok {
		return lower, ""
	}
	return DefaultLogLevel, trimmed
}

// ParseBcryptCost interprets the raw CHAT_BCRYPT_COST env value. An
// empty string returns auth.DefaultBcryptCost with no error (the env
// var is optional and the default is the OWASP floor). A numeric value
// inside [auth.MinBcryptCost, auth.MaxBcryptCost] is returned as-is.
// Anything else — non-integer, below floor, above ceiling — returns a
// non-nil error naming the env var so the operator can fix it. Boot
// code passes the int through to auth.SetBcryptCost; both share the
// same range constants so the two checks cannot drift.
func ParseBcryptCost(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return auth.DefaultBcryptCost, nil
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not an integer: %w", EnvBcryptCost, trimmed, err)
	}
	if n < auth.MinBcryptCost || n > auth.MaxBcryptCost {
		return 0, fmt.Errorf("%s=%d out of range [%d, %d]", EnvBcryptCost, n, auth.MinBcryptCost, auth.MaxBcryptCost)
	}
	return n, nil
}

// LoadTrustedProxy returns the parsed CHAT_TRUSTED_PROXY flag without
// loading the rest of Config. Wiring code that needs only this single
// boolean (e.g. registerAuth's per-IP rate-limit middleware) calls this
// helper to avoid re-parsing every env var on every request boundary.
// Match the AllowPublicBind precedent: strict "1" only.
func LoadTrustedProxy() bool {
	return os.Getenv(EnvTrustedProxy) == "1"
}

// CheckResult names a single startup check and whether it passed.
// On success Validate returns the full slice so the caller can log
// what was verified without echoing any secret value.
type CheckResult struct {
	Name string
	OK   bool
}

// Validate runs every startup check in a fixed order and returns the
// list of checks performed. On the first failure it returns a non-nil
// error whose message is safe to print (no secret material).
func (c Config) Validate() ([]CheckResult, error) {
	var checks []CheckResult

	if err := validateJWTSecret(c.JWTSecret); err != nil {
		return checks, err
	}
	checks = append(checks, CheckResult{Name: "jwt_secret_present_and_strong", OK: true})

	if err := validateInviteCode(c.InviteCode); err != nil {
		return checks, err
	}
	checks = append(checks, CheckResult{Name: "invite_code_present", OK: true})

	if err := validateBind(c.ListenAddr, c.AllowPublicBind); err != nil {
		return checks, err
	}
	checks = append(checks, CheckResult{Name: "bind_address_loopback_or_overridden", OK: true})

	return checks, nil
}

func validateJWTSecret(s string) error {
	if s == "" {
		return fmt.Errorf("%s is required (set a random ASCII string of at least %d bytes)", EnvJWTSecret, MinJWTSecretBytes)
	}
	if len(s) < MinJWTSecretBytes {
		return fmt.Errorf("%s is too short: got %d bytes, need at least %d", EnvJWTSecret, len(s), MinJWTSecretBytes)
	}
	if !isASCII(s) {
		return fmt.Errorf("%s must contain only ASCII bytes", EnvJWTSecret)
	}
	if isRepeatedSingleChar(s) {
		return fmt.Errorf("%s must not be a single character repeated", EnvJWTSecret)
	}
	if isLowEntropy(s) {
		return fmt.Errorf("%s has too few distinct characters; use a random secret", EnvJWTSecret)
	}
	if isDenylisted(s) {
		return fmt.Errorf("%s matches a known dev-default value; use a random secret", EnvJWTSecret)
	}
	return nil
}

func validateInviteCode(s string) error {
	if s == "" {
		return fmt.Errorf("%s is required while registration is enabled", EnvInviteCode)
	}
	return nil
}

func validateBind(addr string, allowPublic bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s=%q is not a valid host:port: %w", EnvListenAddr, addr, err)
	}
	if host == "" {
		if allowPublic {
			return nil
		}
		return fmt.Errorf("%s=%q binds all interfaces; set %s=1 to allow", EnvListenAddr, addr, EnvAllowPublicBind)
	}
	loopback, err := isLoopbackHost(host)
	if err != nil {
		return fmt.Errorf("%s=%q has an unrecognized host: %w", EnvListenAddr, addr, err)
	}
	if loopback {
		return nil
	}
	if allowPublic {
		return nil
	}
	return fmt.Errorf("%s=%q is non-loopback; set %s=1 to allow public bind", EnvListenAddr, addr, EnvAllowPublicBind)
}

func isLoopbackHost(host string) (bool, error) {
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false, errors.New("host is neither an IP nor 'localhost'")
	}
	return ip.IsLoopback(), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func isRepeatedSingleChar(s string) bool {
	if len(s) == 0 {
		return false
	}
	first := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != first {
			return false
		}
	}
	return true
}

// isLowEntropy rejects secrets that meet the length bar but draw on
// fewer than 5 distinct bytes — e.g. "abababab..." or "12121212...".
// Random 32-byte ASCII strings clear this with overwhelming probability.
func isLowEntropy(s string) bool {
	seen := make(map[byte]struct{}, 8)
	for i := 0; i < len(s); i++ {
		seen[s[i]] = struct{}{}
		if len(seen) >= 5 {
			return false
		}
	}
	return true
}

func isDenylisted(s string) bool {
	lower := strings.ToLower(s)
	for _, bad := range jwtSecretDenylist {
		if lower == bad {
			return true
		}
		if strings.HasPrefix(lower, bad) && allSameAfter(lower, len(bad)) {
			return true
		}
	}
	return false
}

// allSameAfter is true when every byte from idx onward equals the byte
// at idx. This catches "change-meXXXXXXXXXXXXXX..." style padding used
// to clear the length floor without picking a real secret.
func allSameAfter(s string, idx int) bool {
	if idx >= len(s) {
		return false
	}
	pad := s[idx]
	for i := idx + 1; i < len(s); i++ {
		if s[i] != pad {
			return false
		}
	}
	return true
}
