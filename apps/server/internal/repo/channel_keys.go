package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ChannelKey mirrors a row in the channel_keys table introduced in
// migration 0006_encryption.sql. Per specs/plans/phase-10/keys.md
// (decision-log §5 + §7): one wrapped root-key copy per
// (channel_id, generation_id, member_user_id). The wrap is a
// 48-byte crypto_box ciphertext (32-byte payload + 16-byte
// Poly1305 MAC); the sender's box pubkey is stored alongside so
// receivers can crypto_box_open without a server round-trip and so
// historical wraps remain decryptable across sender pubkey rotation.
type ChannelKey struct {
	ChannelID       string    `json:"channel_id"`
	GenerationID    int64     `json:"generation_id"`
	MemberUserID    string    `json:"member_user_id"`
	WrappedKey      []byte    `json:"wrapped_key"`
	SenderBoxPubkey []byte    `json:"sender_box_pubkey"`
	Nonce           []byte    `json:"nonce"`
	CreatedAt       time.Time `json:"created_at"`
}

// ErrChannelKeyAlreadyExists surfaces a duplicate wrap row on the
// (channel_id, generation_id, member_user_id) primary key. Atomic
// callers map this to 409 (rotation race-loss; bootstrap re-call).
var ErrChannelKeyAlreadyExists = errors.New(
	"repo: channel_keys row already exists for (channel_id, generation_id, member_user_id)",
)

// InsertChannelKey persists a channel_keys row. Pure storage —
// L30 sender_box_pubkey ownership and L39 byte-length checks live
// in the handler before this call. The handler holds the caller
// identity; this layer cannot resolve "the caller" without the
// http.Request context.
func (r *Repo) InsertChannelKey(ctx context.Context, k ChannelKey) error {
	if r == nil || r.db == nil {
		return errors.New("repo.InsertChannelKey: nil repo or db")
	}
	created := k.CreatedAt.UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_keys(
		    channel_id, generation_id, member_user_id,
		    wrapped_key, sender_box_pubkey, nonce, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ChannelID, k.GenerationID, k.MemberUserID,
		k.WrappedKey, k.SenderBoxPubkey, k.Nonce, created,
	)
	if err != nil && isChannelKeyPKViolation(err) {
		return ErrChannelKeyAlreadyExists
	}
	return err
}

// InsertChannelKeyTx is the *sql.Tx variant of InsertChannelKey for
// callers composing the wrap insert with channel + member inserts in
// one transaction (the L7 atomic-create / atomic-invite invariant).
func (r *Repo) InsertChannelKeyTx(ctx context.Context, tx *sql.Tx, k ChannelKey) error {
	created := k.CreatedAt.UTC()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO channel_keys(
		    channel_id, generation_id, member_user_id,
		    wrapped_key, sender_box_pubkey, nonce, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ChannelID, k.GenerationID, k.MemberUserID,
		k.WrappedKey, k.SenderBoxPubkey, k.Nonce, created,
	)
	if err != nil && isChannelKeyPKViolation(err) {
		return ErrChannelKeyAlreadyExists
	}
	return err
}

// GetChannelKey returns the wrap for (channelID, generationID, memberUserID),
// or (nil, nil) when no row matches.
func (r *Repo) GetChannelKey(ctx context.Context, channelID string, generationID int64, memberUserID string) (*ChannelKey, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT channel_id, generation_id, member_user_id,
		        wrapped_key, sender_box_pubkey, nonce, created_at
		   FROM channel_keys
		  WHERE channel_id = ? AND generation_id = ? AND member_user_id = ?`,
		channelID, generationID, memberUserID,
	)
	var k ChannelKey
	if err := row.Scan(&k.ChannelID, &k.GenerationID, &k.MemberUserID,
		&k.WrappedKey, &k.SenderBoxPubkey, &k.Nonce, &k.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &k, nil
}

// MaxChannelKeyGeneration returns (max(generation_id), found, err) for
// channelID. found == false when no wraps exist (bootstrap state).
// Used by the standalone keys-RPC's three-mode precondition selector
// (decision-log §8 / specs/plans/phase-10/keys.md). This PR uses it on
// the create-flow + add-member paths so the implicit `generation_id`
// can be resolved server-side.
func (r *Repo) MaxChannelKeyGeneration(ctx context.Context, channelID string) (int64, bool, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT MAX(generation_id) FROM channel_keys WHERE channel_id = ?`,
		channelID,
	)
	var n sql.NullInt64
	if err := row.Scan(&n); err != nil {
		return 0, false, err
	}
	if !n.Valid {
		return 0, false, nil
	}
	return n.Int64, true, nil
}

// MaxChannelKeyGenerationTx is the *sql.Tx variant for atomic callers
// that need the current generation as part of a larger transaction.
func (r *Repo) MaxChannelKeyGenerationTx(ctx context.Context, tx *sql.Tx, channelID string) (int64, bool, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT MAX(generation_id) FROM channel_keys WHERE channel_id = ?`,
		channelID,
	)
	var n sql.NullInt64
	if err := row.Scan(&n); err != nil {
		return 0, false, err
	}
	if !n.Valid {
		return 0, false, nil
	}
	return n.Int64, true, nil
}

// ChannelKeyMode discriminates the precondition that the standalone
// keys-RPC (`POST /api/channels/{id}/keys`) selects based on
// generation_id's relationship to MaxChannelKeyGeneration. The body
// shape is identical across modes per specs/plans/phase-10/keys.md
// "Three modes" — only the precondition differs. Server returns
// 400 invalid_generation when the supplied generation matches no mode.
type ChannelKeyMode int

const (
	// ChannelKeyModeInvalid signals "no mode matches" — handler maps
	// to 400 invalid_generation.
	ChannelKeyModeInvalid ChannelKeyMode = iota
	// ChannelKeyModeBootstrap — first wrap for a channel; the caller
	// supplies generation_id == 1 with one wrap-to-self entry.
	ChannelKeyModeBootstrap
	// ChannelKeyModeFillIn — caller supplies generation_id ==
	// MaxGen; wrap-list adds missing wraps for the current generation
	// (lazy-wrap-on-online L14).
	ChannelKeyModeFillIn
	// ChannelKeyModeRotation — caller supplies generation_id ==
	// MaxGen + 1; wrap-list covers every current channel_members row
	// for the new generation.
	ChannelKeyModeRotation
)

// DetectChannelKeyMode resolves the keys-RPC mode from the supplied
// generation_id and the current MaxChannelKeyGeneration for the
// channel. Pure decision: validation of wrap-list shape lives in the
// handler.
//
// Bootstrap fires when no wraps exist yet (hasMax == false) AND the
// caller passed generation_id == 1. CONTR-2 covers the race where
// two users register before either bootstraps — first POST wins, the
// second sees hasMax == true and falls through to fill-in (specs/
// plans/phase-10/keys.md "Bootstrap mode").
func DetectChannelKeyMode(generationID int64, maxGen int64, hasMax bool) ChannelKeyMode {
	if !hasMax {
		if generationID == 1 {
			return ChannelKeyModeBootstrap
		}
		return ChannelKeyModeInvalid
	}
	switch generationID {
	case maxGen:
		return ChannelKeyModeFillIn
	case maxGen + 1:
		return ChannelKeyModeRotation
	default:
		return ChannelKeyModeInvalid
	}
}

// MissingWrapMember describes one (member_user_id, generation_id) pair
// for which channel_members has a row but channel_keys does not — the
// wraps-needed response shape per L22. The handler joins this with
// channel_members to build the MembershipBlock per row.
type MissingWrapMember struct {
	UserID       string
	GenerationID int64
}

// ListMissingWrapsForCurrentGeneration returns one entry per current
// channel_members row that has no matching channel_keys row at the
// channel's current generation (= MaxChannelKeyGeneration). Returns
// (nil, 0, false, nil) when no wraps exist yet for the channel — the
// caller treats that as "bootstrap not yet fired; nothing to fill".
//
// Implementation uses a LEFT JOIN so missing-wrap rows surface as
// channel_keys.member_user_id IS NULL; the join's generation_id pin
// excludes wraps left over from prior generations after a rotation.
func (r *Repo) ListMissingWrapsForCurrentGeneration(
	ctx context.Context, channelID string,
) ([]MissingWrapMember, int64, bool, error) {
	currentGen, hasGen, err := r.MaxChannelKeyGeneration(ctx, channelID)
	if err != nil {
		return nil, 0, false, err
	}
	if !hasGen {
		return nil, 0, false, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT cm.user_id
		   FROM channel_members cm
		   LEFT JOIN channel_keys ck
		     ON ck.channel_id = cm.channel_id
		    AND ck.member_user_id = cm.user_id
		    AND ck.generation_id = ?
		  WHERE cm.channel_id = ?
		    AND ck.member_user_id IS NULL
		  ORDER BY cm.added_at ASC, cm.user_id ASC`,
		currentGen, channelID,
	)
	if err != nil {
		return nil, currentGen, true, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]MissingWrapMember, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, currentGen, true, err
		}
		out = append(out, MissingWrapMember{UserID: uid, GenerationID: currentGen})
	}
	if err := rows.Err(); err != nil {
		return nil, currentGen, true, err
	}
	return out, currentGen, true, nil
}

// isChannelKeyPKViolation maps SQLite's UNIQUE/PRIMARY-KEY message for
// channel_keys to the typed sentinel.
func isChannelKeyPKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "constraint failed") {
		return false
	}
	return strings.Contains(msg, "channel_keys.channel_id") ||
		strings.Contains(msg, "channel_keys.generation_id") ||
		strings.Contains(msg, "channel_keys.member_user_id") ||
		strings.Contains(msg, "PRIMARY KEY")
}
