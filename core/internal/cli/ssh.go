package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/firecracker"
)

type sshRunner func(context.Context, io.Reader, io.Writer, io.Writer, []string) error

func newSSHCommand(opts *rootOptions) *cobra.Command {
	return newSSHCommandWithRunner(opts, runSSH)
}

func newSSHCommandWithRunner(opts *rootOptions, runner sshRunner) *cobra.Command {
	if runner == nil {
		runner = runSSH
	}

	return &cobra.Command{
		Use:   "ssh ENVIRONMENT_ID [-- COMMAND...]",
		Short: "Connect to an environment over SSH",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := apiClient(opts).GetEnvironment(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if env.SSHHost == "" || env.SSHKeyPath == "" {
				return errors.New("environment does not have SSH connection metadata")
			}

			if env.Status != firecracker.StateRunning && env.Status != firecracker.StatePaused {
				return fmt.Errorf("environment status is %q, want running", env.Status)
			}

			port := env.SSHPort
			if port == 0 {
				port = firecracker.SSHPort
			}

			user := env.SSHUser
			if user == "" {
				user = firecracker.SSHUser
			}

			sshArgs := []string{
				"-i", env.SSHKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "LogLevel=ERROR",
				"-p", strconv.Itoa(port),
				user + "@" + env.SSHHost,
			}
			sshArgs = append(sshArgs, args[1:]...)

			return runner(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), sshArgs)
		},
	}
}

func runSSH(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	cmd := exec.CommandContext(ctx, "ssh", args...) //nolint:gosec // SSH target and key are returned by the local Bastion API.
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}
