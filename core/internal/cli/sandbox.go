package cli

import (
	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
)

func newSandboxCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandboxes",
	}
	cmd.AddCommand(
		newSandboxCreateCommand(opts),
		newSandboxListCommand(opts),
		newSandboxPauseCommand(opts),
		newSandboxRemoveCommand(opts),
	)

	return cmd
}

func newSandboxCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		from string
		id   string
		key  string
	)

	cmd := &cobra.Command{
		Use:   "create --from SOURCE [--id ID | --key KEY]",
		Short: "Create a sandbox from a template or checkpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			sandbox, err := apiClient(opts).CreateSandbox(cmd.Context(), sandbox.CreateRequest{
				From: from,
				ID:   id,
				Key:  key,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sandbox)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source type: template or checkpoint")
	cmd.Flags().StringVar(&id, "id", "", "source ID")
	cmd.Flags().StringVar(&key, "key", "", "source key")

	return cmd
}

func newSandboxListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List sandboxes", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListSandboxes(cmd.Context(), limit, cursor)
	})
}

func newSandboxPauseCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "pause SANDBOX_ID",
		Short: "Pause a sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sandbox, err := apiClient(opts).PauseSandbox(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sandbox)
		},
	}
}

func newSandboxRemoveCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove SANDBOX_ID",
		Short: "Remove a sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sandbox, err := apiClient(opts).RemoveSandbox(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sandbox)
		},
	}
}
