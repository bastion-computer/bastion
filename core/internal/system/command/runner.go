// Package command runs host commands for system dependency setup.
package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes host commands needed for system setup.
type Runner interface {
	Run(context.Context, string, ...string) error
}

// ExecRunner executes host commands through os/exec.
type ExecRunner struct{}

// Run executes name with args and returns combined output on failure.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // System setup intentionally runs selected host utilities.

	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	details := strings.TrimSpace(string(output))
	if details == "" {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return fmt.Errorf("%s failed: %w: %s", name, err, details)
}
