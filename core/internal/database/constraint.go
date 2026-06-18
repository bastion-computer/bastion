//go:build cgo

package database

import (
	"errors"

	sqlite "github.com/mattn/go-sqlite3"
)

// IsConstraint reports whether err is a SQLite constraint violation.
func IsConstraint(err error) bool {
	var sqliteErr sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite.ErrConstraint
}
