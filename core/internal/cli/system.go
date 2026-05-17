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

type systemActions struct {
	newRegistry func(string) systemRegistry
}

type systemRegistry interface {
	ResolveDependencies(context.Context) system.Node
	Add(context.Context, string, system.AddOptions) (system.AddResult, error)
	Remove(context.Context, string) (system.RemoveResult, error)
}

type systemOptions struct {
	dataDir string
	actions systemActions
}

func newSystemCommand() *cobra.Command {
	actions := systemActions{
		newRegistry: func(dataDir string) systemRegistry {
			return system.NewRegistry(dataDir)
		},
	}

	return newSystemCommandWithActions(config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir()), actions)
}

func newSystemCommandWithActions(dataDir string, actions systemActions) *cobra.Command {
	if actions.newRegistry == nil {
		actions.newRegistry = func(dataDir string) systemRegistry {
			return system.NewRegistry(dataDir)
		}
	}

	opts := &systemOptions{dataDir: dataDir, actions: actions}
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage host system dependencies",
	}
	cmd.PersistentFlags().StringVar(&opts.dataDir, "data-dir", opts.dataDir, "directory for system assets")
	cmd.AddCommand(
		newSystemCheckCommand(opts),
		newSystemAddCommand(opts),
		newSystemRemoveCommand(opts),
	)

	return cmd
}

func newSystemCheckCommand(opts *systemOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check host system dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := opts.resolvedDataDir()
			if err != nil {
				return err
			}

			registry := opts.actions.newRegistry(dataDir)

			tree := registry.ResolveDependencies(cmd.Context())
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
	var yes bool

	cmd := &cobra.Command{
		Use:   firecrackerDependency,
		Short: "Install Firecracker system assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := opts.resolvedDataDir()
			if err != nil {
				return err
			}

			registry := opts.actions.newRegistry(dataDir)

			result, err := registry.Add(cmd.Context(), firecrackerDependency, system.AddOptions{
				Yes: yes,
				In:  cmd.InOrStdin(),
				Out: cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "installed firecracker system assets in %s\n", result.Path); err != nil {
				return err
			}

			return writeNotes(cmd.OutOrStdout(), result.Notes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "assume yes for non-interactive setup")

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
			dataDir, err := opts.resolvedDataDir()
			if err != nil {
				return err
			}

			registry := opts.actions.newRegistry(dataDir)

			result, err := registry.Remove(cmd.Context(), firecrackerDependency)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed firecracker system assets from %s\n", result.Path); err != nil {
				return err
			}

			return writeNotes(cmd.OutOrStdout(), result.Notes)
		},
	}
}

func (o *systemOptions) resolvedDataDir() (string, error) {
	return config.ExpandPath(o.dataDir)
}

func writeNotes(w io.Writer, notes []string) error {
	for _, note := range notes {
		if _, err := fmt.Fprintf(w, "note: %s\n", note); err != nil {
			return err
		}
	}

	return nil
}
