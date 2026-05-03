// Package config loads and validates server configuration from the
// process environment. Validate() runs once at startup; failures must
// abort the process before any network or DB resource is opened.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Env var names read at startup. Names, not values — gosec's G101
// "hardcoded credentials" check trips on the literal "SECRET" substring.
const (
	EnvJWTSecret       = "CHAT_JWT_SECRET" //nolint:gosec // G101 false positive: env var name, not a credential.
	EnvInviteCode      = "CHAT_INVITE_CODE"
	EnvListenAddr      = "CHAT_LISTEN_ADDR"
	EnvAllowPublicBind = "CHAT_ALLOW_PUBLIC_BIND"

	DefaultListenAddr = "127.0.0.1:8080"

	MinJWTSecretBytes = 32
)

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
}

// Load reads configuration from the environment. It applies defaults
// but does not validate; call Validate() before use.
func Load() Config {
	addr := os.Getenv(EnvListenAddr)
	if addr == "" {
		addr = DefaultListenAddr
	}
	return Config{
		JWTSecret:       os.Getenv(EnvJWTSecret),
		InviteCode:      os.Getenv(EnvInviteCode),
		ListenAddr:      addr,
		AllowPublicBind: os.Getenv(EnvAllowPublicBind) == "1",
	}
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
