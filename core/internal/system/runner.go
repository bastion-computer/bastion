package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// NewExecRunner returns an ExecRunner with default output writers applied.
func NewExecRunner(out, errOut io.Writer) ExecRunner {
	if out == nil {
		out = os.Stdout
	}

	if errOut == nil {
		errOut = os.Stderr
	}

	return ExecRunner{Out: out, Err: errOut}
}

// Run executes name with args and streams command output to configured writers.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // System setup intentionally runs selected host utilities.
	label := commandOutputLabel(name, args)
	cmd.Stdout = newCommandOutputPrefixer(r.Out, label)
	cmd.Stderr = newCommandOutputPrefixer(r.Err, label)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

type commandOutputPrefixer struct {
	out         io.Writer
	prefix      string
	lineStarted bool
}

func newCommandOutputPrefixer(out io.Writer, label string) *commandOutputPrefixer {
	return &commandOutputPrefixer{out: out, prefix: label + ": "}
}

func (w *commandOutputPrefixer) Write(contents []byte) (int, error) {
	if w.out == nil {
		return len(contents), nil
	}

	for _, content := range contents {
		if !w.lineStarted {
			if _, err := io.WriteString(w.out, w.prefix); err != nil {
				return 0, err
			}

			w.lineStarted = true
		}

		if _, err := w.out.Write([]byte{content}); err != nil {
			return 0, err
		}

		if content == '\n' {
			w.lineStarted = false
		}
	}

	return len(contents), nil
}

func commandOutputLabel(name string, args []string) string {
	if name == utilitySudo && len(args) > 0 {
		return filepath.Base(args[0])
	}

	return filepath.Base(name)
}
