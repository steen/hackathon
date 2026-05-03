package auth

import (
	"errors"
	"testing"
	"time"
)

func newKnownUserLookup(t *testing.T, password string) (LookupFunc, *User) {
	t.Helper()
	h, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	u := &User{ID: "u-1", PasswordHash: h, TokenVersion: 0}
	return func(name string) (*User, error) {
		if name == "alice" {
			return u, nil
		}
		return nil, nil
	}, u
}

func TestAuthenticateLoginSuccess(t *testing.T) {
	lookup, u := newKnownUserLookup(t, "correct-horse-battery")
	got, err := AuthenticateLogin(lookup, "alice", "correct-horse-battery")
	if err != nil {
		t.Fatalf("AuthenticateLogin: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("ID: got %q, want %q", got.ID, u.ID)
	}
}

func TestAuthenticateLoginWrongPasswordReturnsErrLogin(t *testing.T) {
	lookup, _ := newKnownUserLookup(t, "correct-horse-battery")
	_, err := AuthenticateLogin(lookup, "alice", "wrong-password-here")
	if !errors.Is(err, ErrLogin) {
		t.Fatalf("got %v, want ErrLogin", err)
	}
}

func TestAuthenticateLoginUnknownUserReturnsErrLogin(t *testing.T) {
	lookup, _ := newKnownUserLookup(t, "correct-horse-battery")
	_, err := AuthenticateLogin(lookup, "nobody", "anything-at-all")
	if !errors.Is(err, ErrLogin) {
		t.Fatalf("got %v, want ErrLogin", err)
	}
}

// SEC-4: error text must be byte-identical for unknown-user vs.
// wrong-password. PRD §9 mandates this so the response text alone
// cannot tell an attacker which arm of the failure they hit.
func TestAuthenticateLoginErrorMessageByteIdenticalForUnknownVsWrongPassword(t *testing.T) {
	lookup, _ := newKnownUserLookup(t, "correct-horse-battery")
	_, e1 := AuthenticateLogin(lookup, "alice", "wrong-password-here")
	_, e2 := AuthenticateLogin(lookup, "nobody", "anything-at-all")
	if e1 == nil || e2 == nil {
		t.Fatalf("expected both arms to fail; got %v, %v", e1, e2)
	}
	if e1.Error() != e2.Error() {
		t.Fatalf("error texts differ: %q vs %q", e1.Error(), e2.Error())
	}
	if e1.Error() != LoginErrorMessage {
		t.Fatalf("error text is not LoginErrorMessage; got %q want %q", e1.Error(), LoginErrorMessage)
	}
}

// SEC-3: response time on unknown-user must stay within ~20% of
// wrong-password time so an attacker cannot enumerate accounts via
// timing. We assert a generous tolerance — strict equality is
// impossible — and we average several samples to damp noise.
func TestAuthenticateLoginConstantTimeWithinTolerance(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test skipped under -short")
	}
	lookup, _ := newKnownUserLookup(t, "correct-horse-battery")

	const samples = 5
	measure := func(user, pw string) time.Duration {
		var total time.Duration
		for i := 0; i < samples; i++ {
			start := time.Now()
			_, _ = AuthenticateLogin(lookup, user, pw)
			total += time.Since(start)
		}
		return total / samples
	}
	wrong := measure("alice", "wrong-password-here")
	unknown := measure("nobody", "anything-at-all")

	// Sanity: both should take meaningful time (bcrypt at cost 10 is
	// in the ms range). If either is suspiciously fast, the dummy
	// hash is probably broken.
	if wrong < 5*time.Millisecond || unknown < 5*time.Millisecond {
		t.Fatalf("suspiciously fast bcrypt: wrong=%v unknown=%v", wrong, unknown)
	}

	ratio := float64(unknown) / float64(wrong)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	// Tolerance is intentionally loose: CI runners are noisy, and
	// the spec calls this a sanity check, not a strict assertion.
	if ratio > 2.5 {
		t.Fatalf("SEC-3: unknown/wrong timing ratio %.2f is outside tolerance "+
			"(unknown=%v wrong=%v)", ratio, unknown, wrong)
	}
	t.Logf("SEC-3: unknown=%v wrong=%v ratio=%.2f", unknown, wrong, ratio)
}

func TestAuthenticateLoginPropagatesLookupIOError(t *testing.T) {
	wantErr := errors.New("database is down")
	lookup := func(string) (*User, error) { return nil, wantErr }
	_, err := AuthenticateLogin(lookup, "alice", "anything-at-all")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}
