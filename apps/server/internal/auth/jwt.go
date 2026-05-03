package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT-related errors. Tests assert against these sentinels rather than
// against jwt-library internals so future library churn is contained.
var (
	ErrJWTSigningKeyEmpty = errors.New("auth: JWT signing key is empty")
	ErrJWTInvalid         = errors.New("auth: invalid token")
	ErrJWTExpired         = errors.New("auth: token expired")
	ErrJWTTokenVersion    = errors.New("auth: token version mismatch")
)

// Claims is the JWT payload. `tv` is the per-user token-version counter
// from the users table; on logout (or any future password change) the
// row's counter is bumped, which makes every previously-issued token
// fail the equality check in Parse and so achieves server-side
// revocation without a deny-list table (PRD §9, US-12).
type Claims struct {
	TokenVersion int `json:"tv"`
	jwt.RegisteredClaims
}

// Issue mints a signed HS256 token for userID at the supplied token
// version. now is taken as a parameter so tests can pin time without
// monkey-patching; production callers pass time.Now().
func Issue(signingKey []byte, userID string, tokenVersion int, now time.Time) (string, error) {
	if len(signingKey) == 0 {
		return "", ErrJWTSigningKeyEmpty
	}
	claims := Claims{
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    JWTIssuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(JWTTTL)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(signingKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// Parse verifies a token signature, expiry, issuer, and token version.
// It returns the parsed claims on success. The current token version is
// supplied by the caller (loaded from the user row) so the package does
// not need a repository dependency.
func Parse(signingKey []byte, tokenStr string, currentTokenVersion int) (*Claims, error) {
	if len(signingKey) == 0 {
		return nil, ErrJWTSigningKeyEmpty
	}
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return signingKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(JWTIssuer))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrJWTExpired
		}
		return nil, ErrJWTInvalid
	}
	if !tok.Valid {
		return nil, ErrJWTInvalid
	}
	if claims.TokenVersion != currentTokenVersion {
		return nil, ErrJWTTokenVersion
	}
	return claims, nil
}
