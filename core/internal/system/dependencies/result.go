package dependencies

import "io"

// AddOptions configures dependency setup.
type AddOptions struct {
	Yes bool
	In  io.Reader
	Out io.Writer
}

// AddResult describes a dependency setup result.
type AddResult struct {
	Path  string
	Notes []string
}

// RemoveResult describes a dependency removal result.
type RemoveResult struct {
	Path  string
	Notes []string
}
