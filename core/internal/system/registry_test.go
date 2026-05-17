package system

import (
	"context"
	"errors"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/system/dependencies"
)

func TestRegistryRoutesDependenciesByName(t *testing.T) {
	t.Parallel()

	dep := &fakeDependency{name: "example"}
	registry := NewRegistryWithDependencies(dep)

	tree := registry.ResolveDependencies(context.Background())
	if !tree.Available() {
		t.Fatalf("tree available = false, want true: %#v", tree)
	}

	if _, err := registry.Add(context.Background(), "example", AddOptions{}); err != nil {
		t.Fatalf("add dependency: %v", err)
	}

	if _, err := registry.Remove(context.Background(), "example"); err != nil {
		t.Fatalf("remove dependency: %v", err)
	}

	if !dep.added || !dep.removed {
		t.Fatalf("dependency added=%v removed=%v, want both true", dep.added, dep.removed)
	}
}

func TestRegistryReportsUnknownDependency(t *testing.T) {
	t.Parallel()

	registry := NewRegistryWithDependencies()

	_, err := registry.Add(context.Background(), "missing", AddOptions{})
	if !errors.Is(err, ErrUnknownDependency) {
		t.Fatalf("add error = %v, want unknown dependency", err)
	}
}

type fakeDependency struct {
	name    string
	added   bool
	removed bool
}

func (d *fakeDependency) Name() string {
	return d.name
}

func (d *fakeDependency) ResolveDependencies(context.Context) dependencies.Node {
	return dependencies.Node{Name: d.name, OK: true}
}

func (d *fakeDependency) Add(context.Context, dependencies.AddOptions) (dependencies.AddResult, error) {
	d.added = true

	return dependencies.AddResult{}, nil
}

func (d *fakeDependency) Remove(context.Context) (dependencies.RemoveResult, error) {
	d.removed = true

	return dependencies.RemoveResult{}, nil
}
