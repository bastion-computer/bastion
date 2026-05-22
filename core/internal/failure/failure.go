// Package failure defines shared domain sentinel errors.
package failure

import "errors"

var (
	// ErrConflict reports a resource conflict.
	ErrConflict = errors.New("conflict")
	// ErrFailedDependency reports a valid request blocked by a failed dependent operation.
	ErrFailedDependency = errors.New("failed dependency")
	// ErrInvalid reports invalid user input.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound reports a missing resource.
	ErrNotFound = errors.New("not found")
)
