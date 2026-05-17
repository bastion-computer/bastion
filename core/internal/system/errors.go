package system

import "errors"

var (
	// ErrMissingDependencies reports a failed system dependency check.
	ErrMissingDependencies = errors.New("error: missing dependencies")
	// ErrUnknownDependency reports an unsupported dependency name.
	ErrUnknownDependency = errors.New("unknown system dependency")
)
