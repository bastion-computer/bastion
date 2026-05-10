// Package cli builds the Bastion command-line interface.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/checkpoint"
)

func newCheckpointsCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoints",
		Short: "Manage checkpoints",
	}
	cmd.AddCommand(
		newCheckpointsCreateCommand(opts),
		newCheckpointsListCommand(opts),
		newCheckpointsRemoveCommand(opts),
	)

	return cmd
}

func newCheckpointsCreateCommand(opts *rootOptions) *cobra.Command {
	var sandboxID string

	cmd := &cobra.Command{
		Use:   "create KEY --sandbox SANDBOX_ID",
		Short: "Create a checkpoint from a paused sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			checkpoint, err := apiClient(opts).CreateCheckpoint(cmd.Context(), checkpoint.CreateRequest{
				Key:       args[0],
				SandboxID: sandboxID,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), checkpoint)
		},
	}
	cmd.Flags().StringVar(&sandboxID, "sandbox", "", "sandbox ID to checkpoint")

	return cmd
}

func newCheckpointsListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List checkpoints", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListCheckpoints(cmd.Context(), limit, cursor)
	})
}

func newCheckpointsRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a checkpoint", "checkpoint ID", "checkpoint key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveCheckpoint(cmd.Context(), id, key)
	})
}
