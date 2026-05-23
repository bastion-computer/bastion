package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/system"
)

const cloudHypervisorDependency = "cloud-hypervisor"

type systemOptions struct {
	dataDir               string
	check                 func(context.Context, string) system.Node
	addCloudHypervisor    func(context.Context, system.AddCloudHypervisorOptions) (system.Result, error)
	removeCloudHypervisor func(context.Context, string) (system.Result, error)
	newRunner             func(io.Writer, io.Writer) system.Runner
}

func newSystemCommand() *cobra.Command {
	return newSystemCommandWithOptions(systemOptions{
		dataDir:               config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir()),
		check:                 system.Check,
		addCloudHypervisor:    system.AddCloudHypervisor,
		removeCloudHypervisor: system.RemoveCloudHypervisor,
		newRunner:             defaultSystemRunner,
	})
}

func newSystemCommandWithOptions(opts systemOptions) *cobra.Command {
	if opts.check == nil {
		opts.check = system.Check
	}

	if opts.addCloudHypervisor == nil {
		opts.addCloudHypervisor = system.AddCloudHypervisor
	}

	if opts.removeCloudHypervisor == nil {
		opts.removeCloudHypervisor = system.RemoveCloudHypervisor
	}

	if opts.newRunner == nil {
		opts.newRunner = defaultSystemRunner
	}

	cmdOpts := &opts
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage host system dependencies",
	}
	cmd.PersistentFlags().StringVar(&cmdOpts.dataDir, "data-dir", cmdOpts.dataDir, "directory for system assets")
	cmd.AddCommand(
		newSystemCheckCommand(cmdOpts),
		newSystemAddCommand(cmdOpts),
		newSystemRemoveCommand(cmdOpts),
	)

	return cmd
}

func newSystemCheckCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check host system dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.dataDir)
			if err != nil {
				return err
			}

			tree := opts.check(cmd.Context(), dataDir)
			if err := tree.Render(cmd.OutOrStdout()); err != nil {
				return err
			}

			if !tree.Available() {
				return system.ErrMissingDependencies
			}

			return nil
		},
	}
}

func newSystemAddCommand(opts *systemOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a host system dependency",
	}
	cmd.AddCommand(newSystemAddCloudHypervisorCommand(opts))

	return cmd
}

func newSystemAddCloudHypervisorCommand(opts *systemOptions) *cobra.Command {
	var withUtilities bool

	cmd := &cobra.Command{
		Use:   cloudHypervisorDependency,
		Short: "Install Cloud Hypervisor system assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.dataDir)
			if err != nil {
				return err
			}

			runner := opts.newRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())

			result, err := opts.addCloudHypervisor(cmd.Context(), system.AddCloudHypervisorOptions{
				DataDir:       dataDir,
				WithUtilities: withUtilities,
				In:            cmd.InOrStdin(),
				Out:           cmd.OutOrStdout(),
				Runner:        runner,
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "installed cloud-hypervisor system assets in %s\n", result.Path); err != nil {
				return err
			}

			return writeSystemNotes(cmd.OutOrStdout(), result.Notes)
		},
	}
	cmd.Flags().BoolVar(&withUtilities, "with-utilities", false, "install missing system utilities without prompting")

	return cmd
}

func newSystemRemoveCommand(opts *systemOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a host system dependency",
	}
	cmd.AddCommand(newSystemRemoveCloudHypervisorCommand(opts))

	return cmd
}

func newSystemRemoveCloudHypervisorCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   cloudHypervisorDependency,
		Short: "Remove Cloud Hypervisor system assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.dataDir)
			if err != nil {
				return err
			}

			result, err := opts.removeCloudHypervisor(cmd.Context(), dataDir)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed cloud-hypervisor system assets from %s\n", result.Path); err != nil {
				return err
			}

			return writeSystemNotes(cmd.OutOrStdout(), result.Notes)
		},
	}
}

func defaultSystemRunner(out, errOut io.Writer) system.Runner {
	return system.NewExecRunner(out, errOut)
}

func writeSystemNotes(w io.Writer, notes []string) error {
	for _, note := range notes {
		if _, err := fmt.Fprintf(w, "note: %s\n", note); err != nil {
			return err
		}
	}

	return nil
}
