package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/system"
	"github.com/bastion-computer/bastion/core/internal/system/command"
)

func TestSystemCheckCommandReturnsMissingDependencies(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: t.TempDir(),
		newRegistryFunc: func(string, command.Runner) systemRegistry {
			return fakeSystemRegistry{
				resolve: func(context.Context) system.Node {
					return system.Node{Name: "bastion", Children: []system.Node{{Name: firecrackerDependency, OK: false}}}
				},
			}
		},
	})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"check"})

	err := cmd.Execute()
	if !errors.Is(err, system.ErrMissingDependencies) {
		t.Fatalf("execute error = %v, want missing dependencies", err)
	}

	if !strings.Contains(out.String(), "bastion [x]") {
		t.Fatalf("check output = %q", out.String())
	}
}

func TestSystemAddFirecrackerCommandPassesWithUtilitiesAndDataDir(t *testing.T) {
	t.Parallel()

	var (
		gotWithUtils bool
		gotDataDir   string
		gotName      string
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		newRegistryFunc: func(dataDir string, _ command.Runner) systemRegistry {
			gotDataDir = dataDir

			return fakeSystemRegistry{
				add: func(_ context.Context, name string, opts system.AddOptions) (system.AddResult, error) {
					gotName = name
					gotWithUtils = opts.WithUtils

					return system.AddResult{Path: dataDir + "/firecracker"}, nil
				},
			}
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--data-dir", dataDir, "add", firecrackerDependency, "--with-utilities"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !gotWithUtils {
		t.Fatal("with utils = false, want true")
	}

	if gotDataDir != dataDir {
		t.Fatalf("data dir = %q, want %q", gotDataDir, dataDir)
	}

	if gotName != firecrackerDependency {
		t.Fatalf("dependency name = %q, want %q", gotName, firecrackerDependency)
	}
}

func TestSystemRemoveFirecrackerCommandPrintsUtilityNote(t *testing.T) {
	t.Parallel()

	var (
		removedDataDir string
		removedName    string
		out            bytes.Buffer
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		newRegistryFunc: func(dataDir string, _ command.Runner) systemRegistry {
			removedDataDir = dataDir

			return fakeSystemRegistry{
				remove: func(_ context.Context, name string) (system.RemoveResult, error) {
					removedName = name

					return system.RemoveResult{
						Path:  dataDir + "/firecracker",
						Notes: []string{"system utilities installed for Firecracker were not removed"},
					}, nil
				},
			}
		},
	})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--data-dir", dataDir, "remove", firecrackerDependency})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if removedDataDir != dataDir {
		t.Fatalf("removed data dir = %q, want %q", removedDataDir, dataDir)
	}

	if removedName != firecrackerDependency {
		t.Fatalf("removed dependency = %q, want %q", removedName, firecrackerDependency)
	}

	if !strings.Contains(out.String(), "note: system utilities installed for Firecracker were not removed") {
		t.Fatalf("remove output = %q", out.String())
	}
}

type fakeSystemRegistry struct {
	resolve func(context.Context) system.Node
	add     func(context.Context, string, system.AddOptions) (system.AddResult, error)
	remove  func(context.Context, string) (system.RemoveResult, error)
}

func (r fakeSystemRegistry) ResolveDependencies(ctx context.Context) system.Node {
	if r.resolve == nil {
		return system.Node{Name: "bastion", OK: true}
	}

	return r.resolve(ctx)
}

func (r fakeSystemRegistry) Add(ctx context.Context, name string, opts system.AddOptions) (system.AddResult, error) {
	if r.add == nil {
		return system.AddResult{}, nil
	}

	return r.add(ctx, name, opts)
}

func (r fakeSystemRegistry) Remove(ctx context.Context, name string) (system.RemoveResult, error) {
	if r.remove == nil {
		return system.RemoveResult{}, nil
	}

	return r.remove(ctx, name)
}
