package cli

import (
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
)

func newClientCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   clientUse,
		Short: "Manage local CLI client configuration",
	}
	cmd.AddCommand(
		newClientSetCommand(opts),
		newClientRemoveCommand(opts),
		newClientConfigCommand(opts),
	)

	return cmd
}

func newClientSetCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   setUse,
		Short: "Set local CLI client configuration overrides",
	}
	cmd.AddCommand(newClientSetAPIURLCommand(opts), newClientSetNamespaceCommand(opts))

	return cmd
}

func newClientSetNamespaceCommand(opts *rootOptions) *cobra.Command {
	return newClientSetValueCommand(opts, rootFlagNamespace+" NAMESPACE", "Persist the cluster namespace used by client commands", validateClientNamespace, func(cfg *config.ClientConfig, value string) {
		cfg.Namespace = value
	})
}

func newClientSetAPIURLCommand(opts *rootOptions) *cobra.Command {
	return newClientSetValueCommand(opts, rootFlagAPIURL+" URL", "Persist the host API URL used by client commands", validateClientAPIURL, func(cfg *config.ClientConfig, value string) {
		cfg.APIURL = value
	})
}

func newClientSetValueCommand(opts *rootOptions, use, short string, validate func(string) error, set func(*config.ClientConfig, string)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			value := args[0]
			if err := validate(value); err != nil {
				return err
			}

			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			set(&clientConfig, value)

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientRemoveCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   removeUse,
		Short: "Remove local CLI client configuration overrides",
	}
	cmd.AddCommand(newClientRemoveAPIURLCommand(opts), newClientRemoveNamespaceCommand(opts))

	return cmd
}

func newClientRemoveNamespaceCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagNamespace,
		Short: "Remove the persisted cluster namespace override",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.Namespace = ""

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientRemoveAPIURLCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagAPIURL,
		Short: "Remove the persisted host API URL override",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.APIURL = ""

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientConfigCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootOptionSourceConfig,
		Short: "Show resolved CLI client configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.clientConfig.APIURL.Value == "" {
				resolved, err := resolveClientConfig(cmd, opts)
				if err != nil {
					return err
				}

				opts.clientConfig = resolved
			}

			return writeJSON(cmd.OutOrStdout(), opts.clientConfig)
		},
	}
}

func validateClientAPIURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("api-url must use http or https")
	}

	if parsed.Host == "" {
		return errors.New("api-url must include a host")
	}

	return nil
}

func validateClientNamespace(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("namespace is required")
	}

	if strings.Contains(value, "/") {
		return errors.New("namespace cannot contain slash")
	}

	return nil
}
