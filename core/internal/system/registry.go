package system

import (
	"context"
	"fmt"

	"github.com/bastion-computer/bastion/core/internal/system/dependencies"
	"github.com/bastion-computer/bastion/core/internal/system/firecracker"
)

const bastionName = "bastion"

// Registry routes system dependency operations by dependency name.
type Registry struct {
	dependencies []dependencies.Installable
	byName       map[string]dependencies.Installable
}

// NewRegistry returns a system dependency registry rooted in dataDir.
func NewRegistry(dataDir string) Registry {
	firecrackerDep := firecracker.NewDependency(dataDir)

	return NewRegistryWithDependencies(firecrackerDep)
}

// NewRegistryWithDependencies returns a registry with explicit dependencies.
func NewRegistryWithDependencies(values ...dependencies.Installable) Registry {
	byName := make(map[string]dependencies.Installable, len(values))
	for _, dependency := range values {
		byName[dependency.Name()] = dependency
	}

	return Registry{dependencies: values, byName: byName}
}

// ResolveDependencies returns the full Bastion system dependency tree.
func (r Registry) ResolveDependencies(ctx context.Context) dependencies.Node {
	children := make([]dependencies.Node, 0, len(r.dependencies))
	for _, dependency := range r.dependencies {
		children = append(children, dependency.ResolveDependencies(ctx))
	}

	return dependencies.Node{Name: bastionName, Children: children}
}

// Add installs a system dependency by name.
func (r Registry) Add(ctx context.Context, name string, opts dependencies.AddOptions) (dependencies.AddResult, error) {
	dependency, ok := r.byName[name]
	if !ok {
		return dependencies.AddResult{}, fmt.Errorf("%w: %s", ErrUnknownDependency, name)
	}

	return dependency.Add(ctx, opts)
}

// Remove removes a system dependency by name.
func (r Registry) Remove(ctx context.Context, name string) (dependencies.RemoveResult, error) {
	dependency, ok := r.byName[name]
	if !ok {
		return dependencies.RemoveResult{}, fmt.Errorf("%w: %s", ErrUnknownDependency, name)
	}

	return dependency.Remove(ctx)
}
