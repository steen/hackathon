// Package repo is the data-access layer for the chat server. Every SQL
// statement in this package is parameterized — no string concatenation —
// per PRD §6 "Parameterized SQL only" and §9 input-handling guarance.
//
// The Repo holds the *sql.DB and exposes methods grouped by domain object
// (users, channels, messages, auth_events). Concrete accessor methods land
// alongside the features that need them; this file only defines the
// constructor so the surface is stable for the migration-bootstrap feature.
package repo

import (
	"database/sql"
	"errors"
)

// Repo is the SQLite-backed data-access façade.
type Repo struct {
	db *sql.DB
}

// New wraps an open *sql.DB. The caller retains ownership and is responsible
// for Close().
func New(sqlDB *sql.DB) (*Repo, error) {
	if sqlDB == nil {
		return nil, errors.New("repo.New: nil *sql.DB")
	}
	return &Repo{db: sqlDB}, nil
}

// DB exposes the underlying handle for callers that need transactions or
// statements not yet wrapped by a typed accessor. Returning the *sql.DB keeps
// us from prematurely abstracting things we do not need.
func (r *Repo) DB() *sql.DB {
	return r.db
}
