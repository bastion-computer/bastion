package cli

import (
	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func newEnvironmentCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
	}
	cmd.AddCommand(
		newEnvironmentCreateCommand(opts),
		newEnvironmentListCommand(opts),
		newEnvironmentGetCommand(opts),
		newEnvironmentRemoveCommand(opts),
	)

	return cmd
}

func newEnvironmentCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		templateID  string
		templateKey string
	)

	cmd := &cobra.Command{
		Use:   "create (--template-id ID | --template KEY)",
		Short: "Create an environment from a template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(templateID, templateKey); err != nil {
				return err
			}

			created, err := apiClient(opts).CreateEnvironment(cmd.Context(), environment.CreateRequest{
				TemplateID:  templateID,
				TemplateKey: templateKey,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&templateID, "template-id", "", "template ID")
	cmd.Flags().StringVar(&templateKey, "template", "", "template key")

	return cmd
}

func newEnvironmentListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List environments", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListEnvironments(cmd.Context(), limit, cursor)
	})
}

func newEnvironmentGetCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ENVIRONMENT_ID",
		Short: "Get an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := apiClient(opts).GetEnvironment(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), env)
		},
	}
}

func newEnvironmentRemoveCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove ENVIRONMENT_ID",
		Short: "Remove an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := apiClient(opts).RemoveEnvironment(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), env)
		},
	}
}
