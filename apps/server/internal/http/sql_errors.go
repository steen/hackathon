package http

import "strings"

// isUniqueViolation returns true when err appears to be SQLite's
// "UNIQUE constraint failed: <table>.<col>" message for the given
// fully-qualified column. We string-match on purpose: modernc/sqlite
// does not export typed sentinels for constraint violations, and we do
// not want to pull in the cgo build tag just to use the sqlite3 codes.
func isUniqueViolation(err error, qualifiedColumn string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, qualifiedColumn)
}
