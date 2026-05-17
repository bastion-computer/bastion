// Package dependencies defines system dependency contracts and status trees.
package dependencies

import "context"

// Dependency is a system dependency that can report its current status.
type Dependency interface {
	Name() string
	ResolveDependencies(context.Context) Node
}

// Installable is a dependency that can be added and removed by Bastion.
type Installable interface {
	Dependency
	Add(context.Context, AddOptions) (AddResult, error)
	Remove(context.Context) (RemoveResult, error)
}
