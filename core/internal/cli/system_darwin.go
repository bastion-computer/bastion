//go:build darwin

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSystemCommand(_ *rootOptions) *cobra.Command {
	run := func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "bastion system is not supported on macOS; use --api-url to connect to a remote Bastion host API")

		return err
	}

	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage host system dependencies",
		RunE:  run,
	}

	check := &cobra.Command{
		Use:   "check",
		Short: "Check host system dependencies",
		Args:  cobra.NoArgs,
		RunE:  run,
	}

	init := &cobra.Command{
		Use:   "init",
		Short: "Install host system dependencies",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	init.Flags().Bool("with-utilities", false, "install missing system utilities without prompting")

	clean := &cobra.Command{
		Use:   "clean",
		Short: "Remove host system dependencies",
		Args:  cobra.NoArgs,
		RunE:  run,
	}

	cmd.AddCommand(check, init, clean)

	return cmd
}
