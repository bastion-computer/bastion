package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/system"
)

func TestSystemCheckCommandReturnsMissingDependencies(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: t.TempDir(),
		check: func(context.Context, string) system.Node {
			return system.Node{Name: "bastion", Children: []system.Node{{Name: firecrackerDependency, OK: false}}}
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
		gotDataDir       string
		gotWithUtilities bool
		gotRunner        system.Runner
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		addFirecracker: func(_ context.Context, opts system.AddFirecrackerOptions) (system.Result, error) {
			gotDataDir = opts.DataDir
			gotWithUtilities = opts.WithUtilities
			gotRunner = opts.Runner

			return system.Result{Path: opts.DataDir + "/firecracker"}, nil
		},
		newRunner: func(io.Writer, io.Writer) system.Runner {
			return fakeCLIRunner{}
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--data-dir", dataDir, "add", firecrackerDependency, "--with-utilities"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if gotDataDir != dataDir {
		t.Fatalf("data dir = %q, want %q", gotDataDir, dataDir)
	}

	if !gotWithUtilities {
		t.Fatal("with utilities = false, want true")
	}

	if gotRunner == nil {
		t.Fatal("runner = nil, want configured runner")
	}
}

func TestSystemRemoveFirecrackerCommandPrintsUtilityNote(t *testing.T) {
	t.Parallel()

	var (
		gotDataDir string
		out        bytes.Buffer
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		removeFirecracker: func(_ context.Context, dataDir string) (system.Result, error) {
			gotDataDir = dataDir

			return system.Result{
				Path:  dataDir + "/firecracker",
				Notes: []string{"system utilities installed for Firecracker were not removed"},
			}, nil
		},
	})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--data-dir", dataDir, "remove", firecrackerDependency})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if gotDataDir != dataDir {
		t.Fatalf("data dir = %q, want %q", gotDataDir, dataDir)
	}

	if !strings.Contains(out.String(), "note: system utilities installed for Firecracker were not removed") {
		t.Fatalf("remove output = %q", out.String())
	}
}

type fakeCLIRunner struct{}

func (fakeCLIRunner) Run(context.Context, string, ...string) error {
	return nil
}
