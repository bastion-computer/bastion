// Package command runs host commands for system dependency setup.
package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Runner executes host commands needed for system setup.
type Runner interface {
	Run(context.Context, string, ...string) error
}

// ExecRunner executes host commands through os/exec.
type ExecRunner struct{}

// Run executes name with args and streams command output to stderr.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // System setup intentionally runs selected host utilities.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}
