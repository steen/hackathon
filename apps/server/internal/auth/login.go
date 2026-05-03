package auth

import "errors"

// ErrLogin is the only error AuthenticateLogin returns on a failed
// attempt. Callers must surface LoginErrorMessage to the client; using
// any other text would let an attacker tell unknown-username from
// wrong-password (PRD §9, SEC-4).
var ErrLogin = errors.New(LoginErrorMessage)

// User is the minimum the auth flow needs from a stored user row.
// Defined here so this package stays decoupled from the repo package.
type User struct {
	ID           string
	PasswordHash string
	TokenVersion int
}

// LookupFunc returns the stored user for username. It must return
// (nil, nil) — not an error — when the user does not exist; an error
// return is reserved for genuine I/O failures. Keeping "not found" out
// of the error channel matters because AuthenticateLogin still needs to
// run the dummy bcrypt comparison in that case.
type LookupFunc func(username string) (*User, error)

// AuthenticateLogin verifies a username/password pair and, on success,
// returns the authenticated user.
//
// Constant-time path: when lookup returns no user, we still call
// VerifyDummy(password) so the response time stays in the same ballpark
// as a real bcrypt comparison (PRD §9, SEC-3). Both failure branches
// return ErrLogin — the message is byte-identical (SEC-4).
//
// The caller decides what to do next (audit-log the failure, mint a
// JWT on success, etc.). Issuing a JWT is intentionally NOT done here
// so this function stays free of the signing key and timekeeping
// dependencies that JWT issuance pulls in.
func AuthenticateLogin(lookup LookupFunc, username, password string) (*User, error) {
	user, err := lookup(username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		VerifyDummy(password)
		return nil, ErrLogin
	}
	if err := Verify(user.PasswordHash, password); err != nil {
		return nil, ErrLogin
	}
	return user, nil
}
