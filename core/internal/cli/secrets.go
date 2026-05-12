package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

func newSecretsCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secret references",
	}
	cmd.AddCommand(
		newSecretsBindCommand(opts),
		newSecretsListCommand(opts),
		newSecretsGetCommand(opts),
		newSecretsResolveCommand(opts),
		newSecretsRemoveCommand(opts),
	)

	return cmd
}

func newSecretsBindCommand(opts *rootOptions) *cobra.Command {
	var allowHosts []string

	cmd := &cobra.Command{
		Use:   "bind KEY:ENV_VAR --allow-host HOST",
		Short: "Bind a secret reference to a host environment variable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, env, ok := strings.Cut(args[0], ":")
			if !ok || key == "" || env == "" {
				return errors.New("expected KEY:ENV_VAR")
			}

			secret, err := apiClient(opts).CreateSecret(cmd.Context(), secret.CreateRequest{
				Key:        key,
				Env:        env,
				AllowHosts: allowHosts,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), secret)
		},
	}
	cmd.Flags().StringArrayVar(&allowHosts, "allow-host", nil, "host allowed to resolve this secret")

	return cmd
}

func newSecretsListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List secret references", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListSecrets(cmd.Context(), limit, cursor)
	})
}

func newSecretsGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a secret reference", "secret ID", "secret key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetSecret(cmd.Context(), id, key)
	})
}

func newSecretsResolveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand("resolve [--id ID | --key KEY]", "Resolve a secret reference from the host environment", "secret ID", "secret key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).ResolveSecret(cmd.Context(), id, key)
	})
}

func newSecretsRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a secret reference", "secret ID", "secret key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveSecret(cmd.Context(), id, key)
	})
}
