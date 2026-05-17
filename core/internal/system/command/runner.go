// Package command runs host commands for system dependency setup.
package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Runner executes host commands needed for system setup.
type Runner interface {
	Run(context.Context, string, ...string) error
}

// ExecRunner executes host commands through os/exec.
type ExecRunner struct {
	Out io.Writer
	Err io.Writer
}

// Run executes name with args and streams command output to configured writers.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	r = r.withDefaults()

	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // System setup intentionally runs selected host utilities.
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

func (r ExecRunner) withDefaults() ExecRunner {
	if r.Out == nil {
		r.Out = os.Stdout
	}

	if r.Err == nil {
		r.Err = os.Stderr
	}

	return r
}
