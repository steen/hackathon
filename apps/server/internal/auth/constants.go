// Package auth holds the password hashing, JWT, and login primitives used
// by the HTTP and WebSocket auth flows.
//
// The constants below are split out so that any change to the policy
// numbers, the bcrypt cost, the JWT TTL, or the dummy hash is visible in
// one place — and so the rest of the package never carries a bare literal.
package auth

import "time"

const (
	// PasswordMinLen is the minimum accepted password length, per PRD §9.
	// PRD §9 says "minimum 10 characters". The acceptance criterion in
	// feature-auth-internals.md mentions "e.g., min 8" as a placeholder;
	// the PRD wins.
	PasswordMinLen = 10

	// PasswordMaxBytes is the hard upper bound. bcrypt silently truncates
	// input past 72 bytes, which would let two distinct passwords hash to
	// the same value — we reject instead so the user sees a real error.
	PasswordMaxBytes = 72

	// DefaultBcryptCost is the cost the dummy hash below was generated at
	// and the default operators get when CHAT_BCRYPT_COST is unset (PRD §9
	// OWASP floor). The runtime cost lives in the BcryptCost variable
	// further down so an operator can raise it via env without recompiling.
	DefaultBcryptCost = 10

	// MinBcryptCost is the floor accepted by SetBcryptCost: PRD §9 / OWASP
	// guidance pins the lowest-permissible bcrypt cost at 10.
	MinBcryptCost = 10

	// MaxBcryptCost is the ceiling accepted by SetBcryptCost: 31 is the
	// upper limit bcrypt.GenerateFromPassword accepts
	// (golang.org/x/crypto/bcrypt rejects above 31 with InvalidCostError).
	MaxBcryptCost = 31

	// JWTTTL is the session token lifetime. PRD §9: 7 days.
	JWTTTL = 7 * 24 * time.Hour

	// JWTIssuer is the value placed in the `iss` claim. Hardcoding this
	// here (rather than threading it through config) keeps verification
	// simple — there is one issuer for the lifetime of the MVP.
	JWTIssuer = "chat-server"
)

// BcryptCost is the runtime cost Hash uses. Defaults to DefaultBcryptCost;
// operators raise it on faster hosts via CHAT_BCRYPT_COST (PRD §9). The
// value is set once at server boot (see SetBcryptCost) before the HTTP
// listener accepts traffic, so the field is read concurrently from
// handlers but never written after startup — the goroutine-start in
// main.go provides the happens-before edge for the Go memory model.
//
// Kept as a package-level var (rather than threaded through AuthDeps)
// so the existing Hash signature stays put and feature wiring code does
// not have to know about a tunable that has one writer (boot) and one
// reader (Register's hash call).
var BcryptCost = DefaultBcryptCost

// LoginErrorMessage is the user-facing text returned for every failed
// login. PRD §9 + SEC-4 require this string to be byte-identical for
// "unknown username" and "wrong password" so an attacker cannot use the
// error text to enumerate accounts. Callers must return this verbatim.
const LoginErrorMessage = "invalid username or password"

// dummyHash is a real bcrypt hash of an unguessable input. The login path
// runs bcrypt.CompareHashAndPassword against this hash whenever the
// supplied username does not exist, so the response time on
// "unknown user" is in the same ballpark as "wrong password" and an
// attacker cannot enumerate usernames via timing (PRD §9, SEC-3).
//
// Generated once with:
//
//	bcrypt.GenerateFromPassword([]byte("never-matches"), bcrypt.DefaultCost)
//
// Pasted here as a const so package init does no work and the cost stays
// stable across builds.
const dummyHash = "$2a$10$B0tIBta89NpNRCXYcw/lYO5SERgfJVJI.2vpJhTg5NBVP9NtxMba2"
