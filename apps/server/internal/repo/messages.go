package repo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"time"
)

// MessageEnvelope is the encrypted-message wire shape from Phase 10
// (decision-log L21, L23). The Go fields hold raw bytes / time.Time so
// SQL Scan and Exec work directly; MarshalJSON / UnmarshalJSON convert
// to / from the base64 + RFC3339 strings the wire spec requires.
//
// Each field maps 1:1 to a column on the messages / dm_messages tables
// per migrations/0007_encrypted_messages_cutover.sql.
//
// CipherSuite is the naclbox-v1 byte (0x01 in v1). KeyGenerationID is
// the per-channel monotonic counter from channel_keys (always 1 for
// DMs since DMs never rotate, decision-log L6). Nonce is 24 raw bytes
// (XSalsa20). Ciphertext is the secretbox payload (variable length).
// SenderSignPubkey + Signature are 32 + 64 raw bytes (Ed25519).
// ClientCreatedAt is a client-stamped, signature-bound timestamp; the
// parent Message.CreatedAt is server-stamped and unsigned.
type MessageEnvelope struct {
	CipherSuite      uint8
	KeyGenerationID  uint32
	Nonce            []byte
	Ciphertext       []byte
	SenderSignPubkey []byte
	Signature        []byte
	ClientCreatedAt  time.Time
}

// envelopeWire is the JSON shape — base64-string fields and an
// RFC3339 ClientCreatedAt — defined here so MarshalJSON and
// UnmarshalJSON share a single struct definition.
type envelopeWire struct {
	CipherSuite      uint8  `json:"cipher_suite"`
	KeyGenerationID  uint32 `json:"key_generation_id"`
	Nonce            string `json:"nonce"`
	Ciphertext       string `json:"ciphertext"`
	SenderSignPubkey string `json:"sender_sign_pubkey"`
	Signature        string `json:"signature"`
	ClientCreatedAt  string `json:"client_created_at"`
}

// MarshalJSON renders MessageEnvelope per the L21 wire shape: each byte
// slice becomes a base64-encoded string and ClientCreatedAt becomes an
// RFC3339 UTC string.
func (e MessageEnvelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(envelopeWire{
		CipherSuite:      e.CipherSuite,
		KeyGenerationID:  e.KeyGenerationID,
		Nonce:            base64.StdEncoding.EncodeToString(e.Nonce),
		Ciphertext:       base64.StdEncoding.EncodeToString(e.Ciphertext),
		SenderSignPubkey: base64.StdEncoding.EncodeToString(e.SenderSignPubkey),
		Signature:        base64.StdEncoding.EncodeToString(e.Signature),
		ClientCreatedAt:  e.ClientCreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

// UnmarshalJSON inverts MarshalJSON for inbound request bodies. It does
// NOT enforce structural validation (length checks, cipher_suite ==
// 0x01) — those checks live in the handler layer per L17 so the
// repo struct can hold a representation that came from the DB rows
// (which may legitimately predate cipher_suite changes in future
// migrations).
func (e *MessageEnvelope) UnmarshalJSON(data []byte) error {
	var w envelopeWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	nonce, err := base64.StdEncoding.DecodeString(w.Nonce)
	if err != nil {
		return err
	}
	ct, err := base64.StdEncoding.DecodeString(w.Ciphertext)
	if err != nil {
		return err
	}
	pub, err := base64.StdEncoding.DecodeString(w.SenderSignPubkey)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(w.Signature)
	if err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339Nano, w.ClientCreatedAt)
	if err != nil {
		// fall back to RFC3339 (no fractional seconds) — the spec says
		// RFC3339 UTC; clients that emit second-precision times still
		// round-trip cleanly.
		t2, err2 := time.Parse(time.RFC3339, w.ClientCreatedAt)
		if err2 != nil {
			return err
		}
		t = t2
	}
	*e = MessageEnvelope{
		CipherSuite:      w.CipherSuite,
		KeyGenerationID:  w.KeyGenerationID,
		Nonce:            nonce,
		Ciphertext:       ct,
		SenderSignPubkey: pub,
		Signature:        sig,
		ClientCreatedAt:  t.UTC(),
	}
	return nil
}

// Message mirrors a row in the messages table. The wire shape (per
// decision-log L21) is `{id, channel_id, sender_user_id, envelope,
// created_at}` — `body` is gone with the 0007 migration.
type Message struct {
	ID           string          `json:"id"`
	ChannelID    string          `json:"channel_id"`
	SenderUserID string          `json:"sender_user_id"`
	Envelope     MessageEnvelope `json:"envelope"`
	CreatedAt    time.Time       `json:"created_at"`
}

// MaxMessagesLimit caps page size so a malicious or buggy client cannot
// stream the entire history in one request.
const MaxMessagesLimit = 200

// DefaultMessagesLimit is used when the caller omits ?limit=.
const DefaultMessagesLimit = 50

// ListMessages returns up to limit messages from channelID, newest first.
// When before is non-empty it acts as an exclusive ULID cursor: only
// messages with id < before are returned. ULID lex order matches
// chronological order, so the same column doubles as the cursor.
func (r *Repo) ListMessages(ctx context.Context, channelID, before string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = DefaultMessagesLimit
	}
	if limit > MaxMessagesLimit {
		limit = MaxMessagesLimit
	}
	const cols = `id, channel_id, user_id,
	              cipher_suite, key_generation_id, nonce, ciphertext,
	              sender_sign_pubkey, signature, client_created_at, created_at`
	var (
		rows *sql.Rows
		err  error
	)
	if before == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+cols+`
			   FROM messages
			  WHERE channel_id = ?
			  ORDER BY id DESC
			  LIMIT ?`,
			channelID, limit)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT `+cols+`
			   FROM messages
			  WHERE channel_id = ? AND id < ?
			  ORDER BY id DESC
			  LIMIT ?`,
			channelID, before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID, &m.ChannelID, &m.SenderUserID,
			&m.Envelope.CipherSuite, &m.Envelope.KeyGenerationID,
			&m.Envelope.Nonce, &m.Envelope.Ciphertext,
			&m.Envelope.SenderSignPubkey, &m.Envelope.Signature,
			&m.Envelope.ClientCreatedAt, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMessageTx persists an encrypted message and atomically updates
// the owning channel's denormalized last_message_id / last_message_at.
// The caller supplies id (ULID), env (server-validated envelope from
// the handler), and now so the broadcast that follows carries the
// same values that landed in the DB. Decision-log L21 + L23.
func (r *Repo) InsertMessageTx(ctx context.Context, id, channelID, userID string, env MessageEnvelope, now time.Time) (*Message, error) {
	created := now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages(
		    id, channel_id, user_id,
		    cipher_suite, key_generation_id, nonce, ciphertext,
		    sender_sign_pubkey, signature, client_created_at, created_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, channelID, userID,
		env.CipherSuite, env.KeyGenerationID, env.Nonce, env.Ciphertext,
		env.SenderSignPubkey, env.Signature, env.ClientCreatedAt.UTC(), created,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE channels SET last_message_id = ?, last_message_at = ? WHERE id = ?`,
		id, created, channelID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Message{
		ID:           id,
		ChannelID:    channelID,
		SenderUserID: userID,
		Envelope:     env,
		CreatedAt:    created,
	}, nil
}
