//go:build darwin

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
)

const cloudHypervisorDependency = "cloud-hypervisor"

func newSystemCommand() *cobra.Command {
	dataDir := config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir())
	run := func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "bastion system is not supported on macOS; use --api-url to connect to a remote Bastion host API")

		return err
	}

	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage host system dependencies",
		RunE:  run,
	}
	cmd.PersistentFlags().StringVar(&dataDir, "data-dir", dataDir, "directory for system assets")

	check := &cobra.Command{
		Use:   "check",
		Short: "Check host system dependencies",
		Args:  cobra.NoArgs,
		RunE:  run,
	}

	addCloudHypervisor := &cobra.Command{
		Use:   cloudHypervisorDependency,
		Short: "Install Cloud Hypervisor system assets",
		Args:  cobra.NoArgs,
		RunE:  run,
	}
	addCloudHypervisor.Flags().Bool("with-utilities", false, "install missing system utilities without prompting")

	add := &cobra.Command{
		Use:   "add",
		Short: "Add a host system dependency",
		RunE:  run,
	}
	add.AddCommand(addCloudHypervisor)

	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove a host system dependency",
		RunE:  run,
	}
	remove.AddCommand(&cobra.Command{
		Use:   cloudHypervisorDependency,
		Short: "Remove Cloud Hypervisor system assets",
		Args:  cobra.NoArgs,
		RunE:  run,
	})

	cmd.AddCommand(check, add, remove)

	return cmd
}
