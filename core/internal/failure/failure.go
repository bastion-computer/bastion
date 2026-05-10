// Package failure defines shared domain sentinel errors.
package failure

import "errors"

var (
	// ErrConflict reports a resource conflict.
	ErrConflict = errors.New("conflict")
	// ErrInvalid reports invalid user input.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound reports a missing resource.
	ErrNotFound = errors.New("not found")
)
