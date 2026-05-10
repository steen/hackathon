package http

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	stdhttp "net/http"
	"time"

	"hackathon/apps/server/internal/auth"
	"hackathon/apps/server/internal/repo"
)

// authStore is a SQL helper used by the auth handlers. Lives in this
// package (not in internal/repo) because the parent feature owns repo
// and froze its surface; concrete accessors land with the feature
// that needs them per the repo package doc.
//
// repo is optional: only CreateUser's auto-join loop needs it (to call
// repo.InsertChannelMemberTx so the L33 NULL-signature guard is single-
// sourced). NewRateLimitAuditSink in middleware_ratelimit.go keeps the
// db-only constructor for the audit-sink path that never touches
// channel_members.
type authStore struct {
	db   *sql.DB
	repo *repo.Repo
}

func newAuthStore(db *sql.DB) *authStore { return &authStore{db: db} }

// newAuthStoreWithRepo wires both the *sql.DB and the *repo.Repo. The
// auth handlers use this constructor so CreateUser can call
// repo.InsertChannelMemberTx instead of inlining its own INSERT.
func newAuthStoreWithRepo(db *sql.DB, r *repo.Repo) *authStore {
	return &authStore{db: db, repo: r}
}

// ErrUsernameTaken is returned by CreateUser when the UNIQUE constraint
// on users.username trips so the handler can return a 409 envelope
// without exposing the SQL error.
var ErrUsernameTaken = errors.New("auth_store: username already taken")

// CreateUser inserts a new user row. Returns ErrUsernameTaken when the
// UNIQUE constraint on users.username trips (covers the case-insensitive
// index added in 0006_encryption.sql).
//
// boxPubkey and signPubkey are the raw 32-byte Phase-10 identity
// pubkeys; pass nil/empty when the caller has not supplied them so the
// columns stay NULL (pre-cutover behaviour, decision-log L26).
//
// Phase-10 §9 + R1.2: the user is auto-added to every existing
// is_public = TRUE channel inside the same transaction so a fresh
// registration cannot observe a window where the row exists but
// #general membership has not landed. The auto-add row uses
// self-invite semantics (inviter_user_id = user_id, inviter_signature
// NULL — accepted by L33's public-channel carve-out). pubkey columns
// pin the new user's keys; on a NULL-pubkey legacy registration
// (decision-log L26) those columns end up empty and the wrap layer in
// #984 will skip the user until they re-register with a Phase-10
// identity. The downstream lazy-wrap-on-online flow (#984) decides
// whether to wrap based on the presence of pubkeys, not on the
// inviter_signature.
func (s *authStore) CreateUser(ctx context.Context, id, username, passwordHash string, boxPubkey, signPubkey []byte, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	created := now.UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, token_version, created_at, box_pubkey, sign_pubkey)
		 VALUES (?, ?, ?, 0, ?, ?, ?)`,
		id, username, passwordHash, created,
		nullableBytes(boxPubkey), nullableBytes(signPubkey),
	); err != nil {
		// modernc/sqlite returns a generic error; we sniff the message
		// for "UNIQUE constraint failed: users.username" rather than
		// adding a driver-specific dependency on its error sentinels.
		// The 0006 migration also adds idx_users_username_nocase, so a
		// case-insensitive duplicate ("Bob" vs. "bob") trips the same
		// branch.
		if isUniqueViolation(err, "users.username") || isUniqueViolation(err, "idx_users_username_nocase") {
			return ErrUsernameTaken
		}
		return err
	}
	// Auto-join every is_public = TRUE channel in the same tx.
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM channels WHERE is_public = TRUE`)
	if err != nil {
		return err
	}
	publicIDs := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			_ = rows.Close()
			return err
		}
		publicIDs = append(publicIDs, s)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(publicIDs) > 0 && s.repo == nil {
		return errors.New("auth_store: CreateUser auto-join requires *repo.Repo; use newAuthStoreWithRepo")
	}
	for _, channelID := range publicIDs {
		// L33 public-channel carve-out: inviter_signature is NULL,
		// inviter_user_id = user_id (self-invite — see decision-log
		// §9). Pubkeys may be empty for a legacy NULL-pubkey
		// registration; the schema requires NOT NULL so we substitute
		// a 32-byte zero placeholder. The lazy-wrap layer (#984)
		// gates wrap computation on the user's actual pubkeys (looked
		// up at wrap time), so the placeholder is a structural
		// satisfaction, not a key claim. Once #983 hard-requires
		// pubkeys at registration this branch goes away.
		boxPin := boxPubkey
		signPin := signPubkey
		if len(boxPin) == 0 {
			boxPin = make([]byte, 32)
		}
		if len(signPin) == 0 {
			signPin = make([]byte, 32)
		}
		// channelIsPublic = true: the SELECT above filtered on
		// is_public = TRUE, so the L33 NULL-signature guard inside
		// repo.InsertChannelMemberTx accepts the self-invite row.
		if err := s.repo.InsertChannelMemberTx(ctx, tx, repo.ChannelMember{
			ChannelID:         channelID,
			UserID:            id,
			InviterUserID:     id,
			InviterSignPubkey: signPin,
			InviteeBoxPubkey:  boxPin,
			InviteeSignPubkey: signPin,
			AddedAt:           created,
		}, true); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// userDetailsRow is the post-Phase-10 row shape the auth handlers
// echo back on register/login/me. Pubkeys are pre-base64-encoded so the
// caller does not have to know the column type. Empty strings stand in
// for SQL NULL.
type userDetailsRow struct {
	username   string
	boxPubkey  string
	signPubkey string
}

// LookupUserDetails returns the username + base64-encoded pubkeys for a
// user id. Returns (nil, nil) when the row does not exist; an empty
// pubkey string stands in for SQL NULL so callers can branch on
// non-empty == populated without a separate sql.NullString check.
func (s *authStore) LookupUserDetails(ctx context.Context, id string) (*userDetailsRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT username, box_pubkey, sign_pubkey FROM users WHERE id = ?`, id)
	var username string
	var box, sign []byte
	if err := row.Scan(&username, &box, &sign); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	out := &userDetailsRow{username: username}
	if len(box) > 0 {
		out.boxPubkey = base64.StdEncoding.EncodeToString(box)
	}
	if len(sign) > 0 {
		out.signPubkey = base64.StdEncoding.EncodeToString(sign)
	}
	return out, nil
}

// nullableBytes is the BLOB-column counterpart to nullableText: an
// empty/nil slice becomes SQL NULL so a row whose pubkeys haven't been
// uploaded yet stays distinguishable from one that explicitly stored
// 0-length bytes.
func nullableBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// LookupForLogin returns the bare-minimum row the auth.AuthenticateLogin
// flow needs. Returns (nil, nil) when the username is unknown so the
// constant-time bcrypt path in auth still runs.
func (s *authStore) LookupForLogin(ctx context.Context, username string) (*auth.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash, token_version FROM users WHERE username = ?`, username)
	var u auth.User
	if err := row.Scan(&u.ID, &u.PasswordHash, &u.TokenVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// LookupUserByID returns the per-request user info the JWT middleware
// needs. Returns (nil, nil) on no-such-user.
func (s *authStore) LookupUserByID(ctx context.Context, id string) (*auth.UserInfo, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, token_version FROM users WHERE id = ?`, id)
	var u auth.UserInfo
	if err := row.Scan(&u.ID, &u.Username, &u.TokenVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// IncrementTokenVersion bumps users.token_version for the given user,
// invalidating every previously-issued JWT (US-12). The new value is
// returned so the caller can issue a fresh token if desired.
func (s *authStore) IncrementTokenVersion(ctx context.Context, userID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET token_version = token_version + 1 WHERE id = ?`, userID); err != nil {
		return 0, err
	}
	var tv int
	if err := tx.QueryRowContext(ctx,
		`SELECT token_version FROM users WHERE id = ?`, userID).Scan(&tv); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return tv, nil
}

// LogRateLimited records one rate-limit rejection in auth_events so
// the spec AC ("Limits are observable in auth_events") holds. The
// signature matches RateLimitAuditSink so the middleware can call it
// without importing the concrete store. Errors are swallowed for the
// same reason LogAuthEvent's callers ignore its error: an audit-log
// failure must not turn a 429 into a 500.
//
// The username is left empty here: the rate-limit middleware fires
// before the request body is decoded, so no attempted username is in
// scope. Per-username rate-limit rejections from the auth handlers
// pass through LogAuthEvent directly with the username they have.
func (s *authStore) LogRateLimited(r *stdhttp.Request, userID, ip string) {
	_ = s.LogAuthEvent(r.Context(), userID, "", AuthEventRateLimited, ip, r.UserAgent())
}

// authEventUsernameMax bounds how many bytes of an attempted username
// land in auth_events.username. SEC-7 caps request bodies at 16KB, so
// without this clamp a user pasting their password into the username
// field on the login form would persist that plaintext into the audit
// column. 64 is 4× the registration regex max (32) — enough to keep
// over-long probes legible for forensics, small enough to bound any
// single-row leak from an audit-DB read. Byte length, not rune count,
// keeps the bound deterministic.
const authEventUsernameMax = 64

// LogAuthEvent appends one auth_events row. Per the migration's column
// set: (user_id NULLABLE, username NULLABLE, kind, ip, ua, at). Either
// userID or username (or both) may be empty — empty strings are stored
// as SQL NULL so queries can distinguish "no value" from "empty
// string". A failed login against an unknown username carries
// username != "" with userID == "", which is the whole point of the
// 0004 migration.
//
// username is truncated to authEventUsernameMax bytes before insert so
// a pasted-password fat-finger cannot smuggle plaintext into the audit
// column. Truncation happens here (not at call sites) so the
// LogRateLimited middleware path inherits the same bound automatically.
func (s *authStore) LogAuthEvent(ctx context.Context, userID, username, kind, ip, ua string) error {
	if len(username) > authEventUsernameMax {
		username = username[:authEventUsernameMax]
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_events(user_id, username, kind, ip, ua, at) VALUES (?, ?, ?, ?, ?, ?)`,
		nullableText(userID), nullableText(username), kind, ip, ua, time.Now().UTC(),
	)
	return err
}

// nullableText returns nil for an empty string so the driver writes
// SQL NULL rather than an empty TEXT value. Keeps NULL-checks in
// audit queries meaningful.
func nullableText(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
