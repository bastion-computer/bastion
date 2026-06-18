package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

func newSecretsCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   secretsUse,
		Short: "Manage secrets",
	}
	cmd.AddCommand(
		newSecretsCreateCommand(opts),
		newSecretsListCommand(opts),
		newSecretsGetCommand(opts),
		newSecretsRemoveCommand(opts),
	)

	return cmd
}

func newSecretsCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		key   string
		value string
	)

	cmd := &cobra.Command{
		Use:   "create [--key KEY] (--value VALUE)",
		Short: "Create a secret",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("value") {
				return errors.New("specify --value")
			}

			var secretKey *string
			if cmd.Flags().Changed("key") {
				secretKey = &key
			}

			created, err := apiClient(opts).CreateSecret(cmd.Context(), secret.CreateRequest{Key: secretKey, Value: value})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique secret key")
	cmd.Flags().StringVar(&value, "value", "", "secret value")

	return cmd
}

func newSecretsListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List secrets", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListSecrets(cmd.Context(), limit, cursor)
	})
}

func newSecretsGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a secret", "secret ID", "secret key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetSecret(cmd.Context(), id, key)
	})
}

func newSecretsRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a secret", "secret ID", "secret key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveSecret(cmd.Context(), id, key)
	})
}
