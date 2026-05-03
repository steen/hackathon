package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testKey = []byte("test-jwt-signing-key-not-for-production-use-32+bytes")

func TestJWTIssueParseRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	tok, err := Issue(testKey, "user-123", 7, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := Parse(testKey, tok, 7)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("Subject: got %q, want user-123", claims.Subject)
	}
	if claims.TokenVersion != 7 {
		t.Fatalf("TokenVersion: got %d, want 7", claims.TokenVersion)
	}
	if claims.Issuer != JWTIssuer {
		t.Fatalf("Issuer: got %q, want %q", claims.Issuer, JWTIssuer)
	}
}

func TestJWTRejectsTamperedSignature(t *testing.T) {
	now := time.Now()
	tok, err := Issue(testKey, "u", 0, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Flip a byte in the middle of the signature segment. Flipping the
	// last char of an unpadded base64url HMAC signature is unreliable
	// because the trailing char carries only 4 useful bits — substituting
	// it for another char with the same high-4-bits decodes to the same
	// signature bytes and Parse legitimately accepts it.
	idx := strings.LastIndex(tok, ".")
	if idx < 0 || idx == len(tok)-1 {
		t.Fatalf("token has no signature segment: %q", tok)
	}
	mid := idx + 1 + (len(tok)-idx-1)/2
	orig := tok[mid]
	flipped := byte('A')
	if orig == 'A' {
		flipped = 'B'
	}
	tampered := tok[:mid] + string(flipped) + tok[mid+1:]
	if _, err := Parse(testKey, tampered, 0); !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("Parse(tampered) = %v, want ErrJWTInvalid", err)
	}
}

func TestJWTRejectsWrongSigningKey(t *testing.T) {
	now := time.Now()
	tok, err := Issue(testKey, "u", 0, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	other := []byte("a-different-key-of-sufficient-length-aaaaaaaaaaaaaa")
	if _, err := Parse(other, tok, 0); !errors.Is(err, ErrJWTInvalid) {
		t.Fatalf("Parse(wrong key) = %v, want ErrJWTInvalid", err)
	}
}

func TestJWTRejectsExpiredToken(t *testing.T) {
	past := time.Now().Add(-2 * JWTTTL)
	tok, err := Issue(testKey, "u", 0, past)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := Parse(testKey, tok, 0); !errors.Is(err, ErrJWTExpired) {
		t.Fatalf("Parse(expired) = %v, want ErrJWTExpired", err)
	}
}

func TestJWTRejectsTokenVersionMismatch(t *testing.T) {
	// US-12: bumping token_version after a logout must invalidate
	// every previously-issued JWT for that user.
	now := time.Now()
	tok, err := Issue(testKey, "u", 3, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := Parse(testKey, tok, 4); !errors.Is(err, ErrJWTTokenVersion) {
		t.Fatalf("Parse(tv mismatch) = %v, want ErrJWTTokenVersion", err)
	}
}

func TestJWTEmptySigningKeyRejected(t *testing.T) {
	if _, err := Issue(nil, "u", 0, time.Now()); !errors.Is(err, ErrJWTSigningKeyEmpty) {
		t.Fatalf("Issue(nil key) = %v, want ErrJWTSigningKeyEmpty", err)
	}
	if _, err := Parse(nil, "x", 0); !errors.Is(err, ErrJWTSigningKeyEmpty) {
		t.Fatalf("Parse(nil key) = %v, want ErrJWTSigningKeyEmpty", err)
	}
}
