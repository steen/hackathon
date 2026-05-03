package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// unverifiedSubject returns the `sub` claim of tokenStr WITHOUT
// verifying the signature, expiry, or token version. Used by the
// middleware to look up the user row before re-parsing the token with
// the row's current `tv`. A signature-bypassing parse is safe here
// because the second pass in Parse is what actually authenticates the
// request — this pass only picks the user row to load.
func unverifiedSubject(tokenStr string) (string, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &Claims{}
	if _, _, err := parser.ParseUnverified(tokenStr, claims); err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", errors.New("auth: empty subject")
	}
	return claims.Subject, nil
}
