package repo_test

import (
	"time"

	"hackathon/apps/server/internal/repo"
)

// fakeEnvelope returns a structurally-valid MessageEnvelope suitable
// for repo-level tests that exercise insert + list paths but do NOT
// care about cryptographic correctness. The byte counts match the
// L17 / L21 / L39 sizes that the http handler validates against:
// nonce 24, sender_sign_pubkey 32, signature 64, ciphertext 1.
//
// Tests that exercise the validateInboundEnvelope path use the http
// package's fixtures instead; this helper is for repo tests where the
// validation has already passed (or doesn't apply).
func fakeEnvelope() repo.MessageEnvelope {
	return repo.MessageEnvelope{
		CipherSuite:      0x01,
		KeyGenerationID:  1,
		Nonce:            make([]byte, 24),
		Ciphertext:       []byte("c"),
		SenderSignPubkey: make([]byte, 32),
		Signature:        make([]byte, 64),
		ClientCreatedAt:  time.Unix(1, 0).UTC(),
	}
}
