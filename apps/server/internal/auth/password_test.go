package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	const pw = "correct-horse-battery-staple"
	h, err := Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h == pw {
		t.Fatal("Hash returned the plaintext")
	}
	if err := Verify(h, pw); err != nil {
		t.Fatalf("Verify(correct): %v", err)
	}
	if err := Verify(h, "wrong-password-here"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Verify(wrong) = %v, want ErrInvalidPassword", err)
	}
}

func TestVerifyMalformedHashIsCollapsedToInvalid(t *testing.T) {
	if err := Verify("not-a-bcrypt-hash", "anything-at-all"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Verify(malformed) = %v, want ErrInvalidPassword", err)
	}
}

func TestEnforcePolicy(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		want error
	}{
		{"too-short empty", "", ErrPasswordTooShort},
		{"too-short by one", strings.Repeat("a", PasswordMinLen-1), ErrPasswordTooShort},
		{"min length ok", strings.Repeat("a", PasswordMinLen), nil},
		{"max bytes ok", strings.Repeat("a", PasswordMaxBytes), nil},
		{"too long by one byte", strings.Repeat("a", PasswordMaxBytes+1), ErrPasswordTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EnforcePolicy(c.pw)
			if !errors.Is(got, c.want) {
				t.Fatalf("EnforcePolicy(%q): got %v, want %v", c.pw, got, c.want)
			}
		})
	}
}

func TestDummyHashIsValidBcrypt(t *testing.T) {
	// Sanity: the const must be a real bcrypt hash; otherwise the
	// constant-time login path would short-circuit on a malformed-hash
	// error and break the SEC-3 timing property.
	VerifyDummy("anything")
	if err := Verify(dummyHash, "anything"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("dummyHash should be a real bcrypt hash that nothing matches; got %v", err)
	}
}
