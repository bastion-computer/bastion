//go:build !cgo

package database

// IsConstraint reports whether err is a SQLite constraint violation.
func IsConstraint(error) bool {
	return false
}
