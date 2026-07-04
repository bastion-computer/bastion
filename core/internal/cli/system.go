//go:build !darwin

package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/system"
)

type systemOptions struct {
	dataDir               string
	dataDirValue          *string
	check                 func(context.Context, string) system.Node
	addCloudHypervisor    func(context.Context, system.AddCloudHypervisorOptions) (system.Result, error)
	addOpenCode           func(context.Context, system.AddOpenCodeOptions) (system.Result, error)
	removeCloudHypervisor func(context.Context, string) (system.Result, error)
	removeOpenCode        func(context.Context, string) (system.Result, error)
	newRunner             func(io.Writer, io.Writer) system.Runner
}

func newSystemCommand(rootOpts *rootOptions) *cobra.Command {
	return newSystemCommandWithOptions(systemOptions{
		dataDir:               rootOpts.dataDir,
		dataDirValue:          &rootOpts.dataDir,
		check:                 system.Check,
		addCloudHypervisor:    system.AddCloudHypervisor,
		addOpenCode:           system.AddOpenCode,
		removeCloudHypervisor: system.RemoveCloudHypervisor,
		removeOpenCode:        system.RemoveOpenCode,
		newRunner:             defaultSystemRunner,
	})
}

func newSystemCommandWithOptions(opts systemOptions) *cobra.Command {
	if opts.dataDir == "" && opts.dataDirValue == nil {
		opts.dataDir = config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir())
	}

	if opts.check == nil {
		opts.check = system.Check
	}

	if opts.addCloudHypervisor == nil {
		opts.addCloudHypervisor = system.AddCloudHypervisor
	}

	if opts.addOpenCode == nil {
		opts.addOpenCode = system.AddOpenCode
	}

	if opts.removeCloudHypervisor == nil {
		opts.removeCloudHypervisor = system.RemoveCloudHypervisor
	}

	if opts.removeOpenCode == nil {
		opts.removeOpenCode = system.RemoveOpenCode
	}

	if opts.newRunner == nil {
		opts.newRunner = defaultSystemRunner
	}

	cmdOpts := &opts

	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage host system dependencies",
	}
	if cmdOpts.dataDirValue == nil {
		cmd.PersistentFlags().StringVar(&cmdOpts.dataDir, rootFlagDataDir, cmdOpts.dataDir, "directory for system assets")
	}

	cmd.AddCommand(
		newSystemCheckCommand(cmdOpts),
		newSystemInitCommand(cmdOpts),
		newSystemCleanCommand(cmdOpts),
	)

	return cmd
}

func newSystemCheckCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check host system dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.currentDataDir())
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

func newSystemInitCommand(opts *systemOptions) *cobra.Command {
	var withUtilities bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Install host system dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.currentDataDir())
			if err != nil {
				return err
			}

			runner := opts.newRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())

			cloudHypervisorResult, err := opts.addCloudHypervisor(cmd.Context(), system.AddCloudHypervisorOptions{
				DataDir:       dataDir,
				WithUtilities: withUtilities,
				In:            cmd.InOrStdin(),
				Out:           cmd.OutOrStdout(),
				Runner:        runner,
			})
			if err != nil {
				return err
			}

			openCodeResult, err := opts.addOpenCode(cmd.Context(), system.AddOpenCodeOptions{
				DataDir: dataDir,
				Out:     cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "installed system dependencies in %s\n", dataDir); err != nil {
				return err
			}

			return writeSystemNotes(cmd.OutOrStdout(), append(cloudHypervisorResult.Notes, openCodeResult.Notes...))
		},
	}
	cmd.Flags().BoolVar(&withUtilities, "with-utilities", false, "install missing system utilities without prompting")

	return cmd
}

func newSystemCleanCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove host system dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := config.ExpandPath(opts.currentDataDir())
			if err != nil {
				return err
			}

			cloudHypervisorResult, err := opts.removeCloudHypervisor(cmd.Context(), dataDir)
			if err != nil {
				return err
			}

			openCodeResult, err := opts.removeOpenCode(cmd.Context(), dataDir)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed system dependencies from %s\n", dataDir); err != nil {
				return err
			}

			return writeSystemNotes(cmd.OutOrStdout(), append(cloudHypervisorResult.Notes, openCodeResult.Notes...))
		},
	}
}

func defaultSystemRunner(out, errOut io.Writer) system.Runner {
	return system.NewExecRunner(out, errOut)
}

func (opts *systemOptions) currentDataDir() string {
	if opts.dataDirValue != nil {
		return *opts.dataDirValue
	}

	return opts.dataDir
}

func writeSystemNotes(w io.Writer, notes []string) error {
	for _, note := range notes {
		if _, err := fmt.Fprintf(w, "note: %s\n", note); err != nil {
			return err
		}
	}

	return nil
}
