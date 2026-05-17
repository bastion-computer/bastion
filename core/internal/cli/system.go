package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/system"
)

const firecrackerDependency = "firecracker"

type systemOptions struct {
	dataDir           string
	check             func(context.Context, string) system.Node
	addFirecracker    func(context.Context, system.AddFirecrackerOptions) (system.Result, error)
	removeFirecracker func(context.Context, string) (system.Result, error)
	newRunner         func(io.Writer, io.Writer) system.Runner
}

func newSystemCommand() *cobra.Command {
	return newSystemCommandWithOptions(systemOptions{
		dataDir:           config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir()),
		check:             system.Check,
		addFirecracker:    system.AddFirecracker,
		removeFirecracker: system.RemoveFirecracker,
		newRunner:         defaultSystemRunner,
	})
}

func newSystemCommandWithOptions(opts systemOptions) *cobra.Command {
	if opts.check == nil {
		opts.check = system.Check
	}

	if opts.addFirecracker == nil {
		opts.addFirecracker = system.AddFirecracker
	}

	if opts.removeFirecracker == nil {
		opts.removeFirecracker = system.RemoveFirecracker
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
	cmd.AddCommand(newSystemAddFirecrackerCommand(opts))

	return cmd
}

func newSystemAddFirecrackerCommand(opts *systemOptions) *cobra.Command {
	var withUtilities bool

	cmd := &cobra.Command{
		Use:   firecrackerDependency,
		Short: "Install Firecracker system assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.dataDir)
			if err != nil {
				return err
			}

			runner := opts.newRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())

			result, err := opts.addFirecracker(cmd.Context(), system.AddFirecrackerOptions{
				DataDir:       dataDir,
				WithUtilities: withUtilities,
				In:            cmd.InOrStdin(),
				Out:           cmd.OutOrStdout(),
				Runner:        runner,
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "installed firecracker system assets in %s\n", result.Path); err != nil {
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
	cmd.AddCommand(newSystemRemoveFirecrackerCommand(opts))

	return cmd
}

func newSystemRemoveFirecrackerCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   firecrackerDependency,
		Short: "Remove Firecracker system assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.dataDir)
			if err != nil {
				return err
			}

			result, err := opts.removeFirecracker(cmd.Context(), dataDir)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed firecracker system assets from %s\n", result.Path); err != nil {
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
