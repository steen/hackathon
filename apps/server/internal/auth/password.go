package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// Errors returned by Hash, Verify, and EnforcePolicy. They are typed so
// callers can branch (e.g. handlers translate ErrPasswordTooShort to a
// 400 with a helpful message), but ErrInvalidPassword is the only one
// the login path is allowed to surface — anything more specific would
// help an attacker tell "wrong user" from "wrong password" (SEC-4).
var (
	ErrPasswordTooShort = errors.New("auth: password shorter than minimum")
	ErrPasswordTooLong  = errors.New("auth: password exceeds bcrypt 72-byte limit")
	ErrInvalidPassword  = errors.New("auth: invalid password")
)

// EnforcePolicy validates a candidate password against the length policy
// from PRD §9. The byte length (not rune count) is what matters because
// bcrypt's 72-byte limit is on the input bytes.
func EnforcePolicy(password string) error {
	if len(password) < PasswordMinLen {
		return ErrPasswordTooShort
	}
	if len(password) > PasswordMaxBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// Hash returns a bcrypt hash of password using BcryptCost. It does NOT
// enforce the policy — call EnforcePolicy first at the handler boundary.
// Keeping policy out of Hash means tests and migrations can rehash legacy
// values without tripping over a stricter policy.
func Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Verify checks whether password matches hash. It returns nil on a
// successful match and ErrInvalidPassword on any mismatch (including a
// malformed hash). The bcrypt comparison itself is constant-time across
// candidates of the same length; the wrapper deliberately collapses
// every failure mode into a single sentinel so callers cannot leak
// "your hash is malformed" vs. "wrong password" via the error type.
func Verify(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return nil
	}
	return ErrInvalidPassword
}

// VerifyDummy runs a bcrypt comparison against the package-level dummy
// hash so the login code path takes a similar amount of time when the
// username is unknown as when the password is wrong. The return value
// is intentionally discarded by the caller: the comparison is performed
// for its timing side-effect, not for its result.
//
// Exported (rather than inlined into AuthenticateLogin) so the
// constant-time-tolerance test can call it directly.
func VerifyDummy(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
}
