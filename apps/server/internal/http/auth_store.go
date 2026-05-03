package http

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hackathon/apps/server/internal/auth"
)

// authStore is a SQL helper used by the auth handlers. Lives in this
// package (not in internal/repo) because the parent feature owns repo
// and froze its surface; concrete accessors land with the feature
// that needs them per the repo package doc.
type authStore struct{ db *sql.DB }

func newAuthStore(db *sql.DB) *authStore { return &authStore{db: db} }

// CreateUser inserts a new user row. Returns ErrUsernameTaken when the
// UNIQUE constraint on users.username trips so the handler can return
// a 409 envelope without exposing the SQL error.
var ErrUsernameTaken = errors.New("auth_store: username already taken")

func (s *authStore) CreateUser(ctx context.Context, id, username, passwordHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, token_version, created_at)
		 VALUES (?, ?, ?, 0, ?)`,
		id, username, passwordHash, now.UTC(),
	)
	if err != nil {
		// modernc/sqlite returns a generic error; we sniff the message
		// for "UNIQUE constraint failed: users.username" rather than
		// adding a driver-specific dependency on its error sentinels.
		if isUniqueViolation(err, "users.username") {
			return ErrUsernameTaken
		}
		return err
	}
	return nil
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

// LogAuthEvent appends one auth_events row. Per the migration's column
// set: (user_id NULLABLE, kind, ip, ua, at). userID may be empty for a
// failed login against an unknown username.
func (s *authStore) LogAuthEvent(ctx context.Context, userID, kind, ip, ua string) error {
	var u interface{}
	if userID != "" {
		u = userID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_events(user_id, kind, ip, ua, at) VALUES (?, ?, ?, ?, ?)`,
		u, kind, ip, ua, time.Now().UTC(),
	)
	return err
}
