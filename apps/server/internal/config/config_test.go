package config

import (
	"strings"
	"testing"
)

const validSecret = "kf8Q2nZx7vP1aLm3RbT9oYwH6sJgC4dE"

func baseValid() Config {
	return Config{
		JWTSecret:       validSecret,
		InviteCode:      "team-invite-2026",
		ListenAddr:      "127.0.0.1:8080",
		AllowPublicBind: false,
	}
}

func TestValidate_AcceptsBaseline(t *testing.T) {
	checks, err := baseValid().Validate()
	if err != nil {
		t.Fatalf("baseline config rejected: %v", err)
	}
	want := []string{
		"jwt_secret_present_and_strong",
		"invite_code_present",
		"bind_address_loopback_or_overridden",
	}
	if len(checks) != len(want) {
		t.Fatalf("want %d checks, got %d (%v)", len(want), len(checks), checks)
	}
	for i, name := range want {
		if checks[i].Name != name || !checks[i].OK {
			t.Errorf("check[%d] = %+v, want name=%q ok=true", i, checks[i], name)
		}
	}
}

// SEC-1: server refuses to start with missing/short/denylisted CHAT_JWT_SECRET.

func TestSEC1_RejectsMissingJWTSecret(t *testing.T) {
	c := baseValid()
	c.JWTSecret = ""
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected error on missing JWT secret")
	}
	if !strings.Contains(err.Error(), EnvJWTSecret) {
		t.Errorf("error should name the env var, got %q", err.Error())
	}
}

func TestSEC1_RejectsShortJWTSecret(t *testing.T) {
	for _, n := range []int{1, 8, 16, 31} {
		c := baseValid()
		c.JWTSecret = strings.Repeat("a", n)
		_, err := c.Validate()
		if err == nil {
			t.Errorf("len=%d: expected error", n)
		}
	}
}

func TestSEC1_RejectsDenylistedJWTSecret(t *testing.T) {
	cases := []string{
		"change-me",
		"Change-Me",
		"CHANGEME",
		"secret",
		"dev",
		"password",
		"hackathon",
	}
	for _, s := range cases {
		padded := s + strings.Repeat(string(s[len(s)-1]), MinJWTSecretBytes)
		c := baseValid()
		c.JWTSecret = padded
		_, err := c.Validate()
		if err == nil {
			t.Errorf("denylist %q: expected rejection", s)
			continue
		}
		if strings.Contains(err.Error(), padded) {
			t.Errorf("denylist %q: error leaks secret %q", s, padded)
		}
	}
}

func TestSEC1_RejectsRepeatedSingleChar(t *testing.T) {
	c := baseValid()
	c.JWTSecret = strings.Repeat("x", 64)
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected rejection for repeated single character")
	}
	if strings.Contains(err.Error(), c.JWTSecret) {
		t.Error("error leaks secret value")
	}
}

func TestSEC1_RejectsLowEntropySecret(t *testing.T) {
	c := baseValid()
	c.JWTSecret = strings.Repeat("ab", 32)
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected rejection for low-entropy secret")
	}
}

func TestSEC1_RejectsNonASCIISecret(t *testing.T) {
	c := baseValid()
	c.JWTSecret = strings.Repeat("é", 32)
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected rejection for non-ASCII secret")
	}
}

func TestSEC1_AcceptsRandom32ByteSecret(t *testing.T) {
	c := baseValid()
	c.JWTSecret = validSecret
	if _, err := c.Validate(); err != nil {
		t.Fatalf("expected acceptance, got %v", err)
	}
}

// SEC-2: server refuses non-loopback bind unless CHAT_ALLOW_PUBLIC_BIND=1.

func TestSEC2_RejectsPublicBindWithoutOverride(t *testing.T) {
	cases := []string{
		"0.0.0.0:8080",
		"192.168.1.10:8080",
		"10.0.0.5:8080",
		"[::]:8080",
		"[2001:db8::1]:8080",
		":8080",
	}
	for _, addr := range cases {
		c := baseValid()
		c.ListenAddr = addr
		c.AllowPublicBind = false
		_, err := c.Validate()
		if err == nil {
			t.Errorf("addr %q: expected rejection", addr)
			continue
		}
		if !strings.Contains(err.Error(), EnvAllowPublicBind) {
			t.Errorf("addr %q: error should mention %s, got %q", addr, EnvAllowPublicBind, err.Error())
		}
	}
}

func TestSEC2_AllowsPublicBindWhenOverrideSet(t *testing.T) {
	cases := []string{
		"0.0.0.0:8080",
		"192.168.1.10:8080",
		"[::]:8080",
		":8080",
	}
	for _, addr := range cases {
		c := baseValid()
		c.ListenAddr = addr
		c.AllowPublicBind = true
		if _, err := c.Validate(); err != nil {
			t.Errorf("addr %q with override: expected acceptance, got %v", addr, err)
		}
	}
}

func TestSEC2_AllowsLoopbackBindByDefault(t *testing.T) {
	cases := []string{
		"127.0.0.1:8080",
		"127.0.0.5:8080",
		"[::1]:8080",
		"localhost:8080",
	}
	for _, addr := range cases {
		c := baseValid()
		c.ListenAddr = addr
		if _, err := c.Validate(); err != nil {
			t.Errorf("addr %q: expected acceptance, got %v", addr, err)
		}
	}
}

func TestSEC2_RejectsMalformedAddr(t *testing.T) {
	c := baseValid()
	c.ListenAddr = "not-a-host-port"
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected rejection for malformed addr")
	}
}

// US-11 startup enforcement: invite code required.

func TestUS11_RejectsMissingInviteCode(t *testing.T) {
	c := baseValid()
	c.InviteCode = ""
	_, err := c.Validate()
	if err == nil {
		t.Fatal("expected rejection for missing invite code")
	}
	if !strings.Contains(err.Error(), EnvInviteCode) {
		t.Errorf("error should mention %s, got %q", EnvInviteCode, err.Error())
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	t.Setenv(EnvJWTSecret, "")
	t.Setenv(EnvInviteCode, "")
	t.Setenv(EnvListenAddr, "")
	t.Setenv(EnvAllowPublicBind, "")
	c := Load()
	if c.ListenAddr != DefaultListenAddr {
		t.Errorf("default ListenAddr = %q, want %q", c.ListenAddr, DefaultListenAddr)
	}
	if c.AllowPublicBind {
		t.Error("AllowPublicBind should default to false")
	}
}

func TestLoad_ReadsEnv(t *testing.T) {
	t.Setenv(EnvJWTSecret, validSecret)
	t.Setenv(EnvInviteCode, "abc")
	t.Setenv(EnvListenAddr, "0.0.0.0:9000")
	t.Setenv(EnvAllowPublicBind, "1")
	c := Load()
	if c.JWTSecret != validSecret {
		t.Errorf("JWTSecret not loaded")
	}
	if c.InviteCode != "abc" {
		t.Errorf("InviteCode = %q, want %q", c.InviteCode, "abc")
	}
	if c.ListenAddr != "0.0.0.0:9000" {
		t.Errorf("ListenAddr = %q", c.ListenAddr)
	}
	if !c.AllowPublicBind {
		t.Error("AllowPublicBind should be true when env=1")
	}
}

func TestLoad_AllowPublicBind_OnlyAcceptsOne(t *testing.T) {
	for _, v := range []string{"true", "yes", "TRUE", "0", "", "2"} {
		t.Setenv(EnvAllowPublicBind, v)
		c := Load()
		if c.AllowPublicBind {
			t.Errorf("AllowPublicBind=%q should be false (only \"1\" enables it)", v)
		}
	}
}

func TestLoad_LogLevel_DefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvLogLevel, "")
	c := Load()
	if c.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", c.LogLevel, DefaultLogLevel)
	}
	if c.LogLevelInvalid != "" {
		t.Errorf("LogLevelInvalid = %q, want empty for unset env", c.LogLevelInvalid)
	}
}

func TestLoad_LogLevel_AcceptsCanonicalValues(t *testing.T) {
	for _, v := range []string{"debug", "info", "warn", "error"} {
		t.Setenv(EnvLogLevel, v)
		c := Load()
		if c.LogLevel != v {
			t.Errorf("LogLevel(%q) = %q, want %q", v, c.LogLevel, v)
		}
		if c.LogLevelInvalid != "" {
			t.Errorf("LogLevel(%q) marked invalid: %q", v, c.LogLevelInvalid)
		}
	}
}

func TestLoad_LogLevel_CaseInsensitive(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"DEBUG", "debug"},
		{"Info", "info"},
		{"  WARN ", "warn"},
		{"Error", "error"},
	}
	for _, tc := range cases {
		t.Setenv(EnvLogLevel, tc.raw)
		c := Load()
		if c.LogLevel != tc.want {
			t.Errorf("LogLevel(%q) = %q, want %q", tc.raw, c.LogLevel, tc.want)
		}
		if c.LogLevelInvalid != "" {
			t.Errorf("LogLevel(%q) marked invalid: %q", tc.raw, c.LogLevelInvalid)
		}
	}
}

func TestLoad_LogLevel_UnknownFallsBackAndMarksInvalid(t *testing.T) {
	for _, v := range []string{"verbose", "trace", "warning", "fatal"} {
		t.Setenv(EnvLogLevel, v)
		c := Load()
		if c.LogLevel != DefaultLogLevel {
			t.Errorf("LogLevel(%q) = %q, want fallback %q", v, c.LogLevel, DefaultLogLevel)
		}
		if c.LogLevelInvalid != v {
			t.Errorf("LogLevelInvalid(%q) = %q, want %q", v, c.LogLevelInvalid, v)
		}
	}
}

func TestErrorsNeverContainSecret(t *testing.T) {
	// Use distinctive secrets unlikely to appear as natural English
	// substrings of the error template, so a positive substring match
	// indicates real leakage rather than coincidence.
	secrets := []string{
		"Zq7Rk2Vp9Wn1Bx5Lc8Ud3Mh6Tg4Yj0Aw",
		strings.Repeat("Q", 64),
		strings.Repeat("Qz", 32),
		"change-me" + strings.Repeat("Z", 32),
	}
	for _, s := range secrets {
		c := baseValid()
		c.JWTSecret = s
		_, err := c.Validate()
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), s) {
			t.Errorf("error message contains the secret value %q: %q", s, err.Error())
		}
	}
}
