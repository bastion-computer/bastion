//go:build !darwin

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
			return system.Node{Name: "bastion", Children: []system.Node{{Name: cloudHypervisorDependency, OK: false}}}
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

func TestSystemAddCloudHypervisorCommandPassesWithUtilitiesAndDataDir(t *testing.T) {
	t.Parallel()

	var (
		gotDataDir       string
		gotWithUtilities bool
		gotRunner        system.Runner
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		addCloudHypervisor: func(_ context.Context, opts system.AddCloudHypervisorOptions) (system.Result, error) {
			gotDataDir = opts.DataDir
			gotWithUtilities = opts.WithUtilities
			gotRunner = opts.Runner

			return system.Result{Path: opts.DataDir + "/cloud-hypervisor"}, nil
		},
		newRunner: func(io.Writer, io.Writer) system.Runner {
			return fakeCLIRunner{}
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestDataDirFlag, dataDir, "add", cloudHypervisorDependency, "--with-utilities"})

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

func TestSystemRemoveCloudHypervisorCommandPrintsUtilityNote(t *testing.T) {
	t.Parallel()

	var (
		gotDataDir string
		out        bytes.Buffer
	)

	dataDir := t.TempDir()
	cmd := newSystemCommandWithOptions(systemOptions{
		dataDir: "unused",
		removeCloudHypervisor: func(_ context.Context, dataDir string) (system.Result, error) {
			gotDataDir = dataDir

			return system.Result{
				Path:  dataDir + "/cloud-hypervisor",
				Notes: []string{"system utilities installed for Cloud Hypervisor were not removed"},
			}, nil
		},
	})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestDataDirFlag, dataDir, removeUse, cloudHypervisorDependency})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if gotDataDir != dataDir {
		t.Fatalf("data dir = %q, want %q", gotDataDir, dataDir)
	}

	if !strings.Contains(out.String(), "note: system utilities installed for Cloud Hypervisor were not removed") {
		t.Fatalf("remove output = %q", out.String())
	}
}

type fakeCLIRunner struct{}

func (fakeCLIRunner) Run(context.Context, string, ...string) error {
	return nil
}
