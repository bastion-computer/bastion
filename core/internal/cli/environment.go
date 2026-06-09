package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func newEnvironmentCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   environmentUse,
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
		key         string
		templateID  string
		templateKey string
		tags        []string
	)

	cmd := &cobra.Command{
		Use:   "create (--template-id ID | --template-key KEY) [--key KEY]",
		Short: "Create an environment from a template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (templateID == "") == (templateKey == "") {
				return errors.New("specify exactly one of --template-id or --template-key")
			}

			var environmentKey *string
			if cmd.Flags().Changed("key") {
				environmentKey = &key
			}

			created, err := apiClient(opts).CreateEnvironment(cmd.Context(), environment.CreateRequest{
				Key:         environmentKey,
				TemplateID:  templateID,
				TemplateKey: templateKey,
				Tags:        tags,
				Logs:        cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique environment key")
	cmd.Flags().StringVar(&templateID, "template-id", "", "template ID")
	cmd.Flags().StringVar(&templateKey, "template-key", "", "template key")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", nil, "environment tag (repeatable)")

	return cmd
}

func newEnvironmentListCommand(opts *rootOptions) *cobra.Command {
	var (
		limit  int
		cursor string
		tags   []string
	)

	cmd := &cobra.Command{
		Use:   listUse,
		Short: "List environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := apiClient(opts).ListEnvironments(cmd.Context(), limit, cursor, tags)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), value)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum entries to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", nil, "environment tag filter (repeatable)")

	return cmd
}

func newEnvironmentGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get an environment", "environment ID", "environment key", func(cmd *cobra.Command, id, key string) (any, error) {
		if key != "" {
			return apiClient(opts).GetEnvironmentByKey(cmd.Context(), key)
		}

		return apiClient(opts).GetEnvironment(cmd.Context(), id)
	})
}

func newEnvironmentRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove an environment", "environment ID", "environment key", func(cmd *cobra.Command, id, key string) (any, error) {
		if key != "" {
			return apiClient(opts).RemoveEnvironmentByKey(cmd.Context(), key)
		}

		return apiClient(opts).RemoveEnvironment(cmd.Context(), id)
	})
}
