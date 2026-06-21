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
	cmd.AddCommand(
		newClientSetAPIURLCommand(opts),
		newClientSetNamespaceIDCommand(opts),
		newClientSetNamespaceKeyCommand(opts),
	)

	return cmd
}

func newClientSetNamespaceIDCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagNamespaceID + " ID",
		Short: "Persist the cluster namespace ID used by resource commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			namespaceID := args[0]
			if err := validateClientNamespace(rootFlagNamespaceID, namespaceID); err != nil {
				return err
			}

			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.NamespaceID = namespaceID
			clientConfig.NamespaceKey = ""

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientSetNamespaceKeyCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagNamespaceKey + " KEY",
		Short: "Persist the cluster namespace key used by resource commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			namespaceKey := args[0]
			if err := validateClientNamespace(rootFlagNamespaceKey, namespaceKey); err != nil {
				return err
			}

			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.NamespaceID = ""
			clientConfig.NamespaceKey = namespaceKey

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientSetAPIURLCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagAPIURL + " URL",
		Short: "Persist the host API URL used by client commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			apiURL := args[0]
			if err := validateClientAPIURL(apiURL); err != nil {
				return err
			}

			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.APIURL = apiURL

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientRemoveCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   removeUse,
		Short: "Remove local CLI client configuration overrides",
	}
	cmd.AddCommand(
		newClientRemoveAPIURLCommand(opts),
		newClientRemoveNamespaceIDCommand(opts),
		newClientRemoveNamespaceKeyCommand(opts),
	)

	return cmd
}

func newClientRemoveNamespaceIDCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagNamespaceID,
		Short: "Remove the persisted cluster namespace ID override",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.NamespaceID = ""

			return config.SaveClientConfig(opts.dataDir, clientConfig)
		},
	}
}

func newClientRemoveNamespaceKeyCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   rootFlagNamespaceKey,
		Short: "Remove the persisted cluster namespace key override",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			clientConfig, err := config.LoadClientConfig(opts.dataDir)
			if err != nil {
				return err
			}

			clientConfig.NamespaceKey = ""

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

func validateClientNamespace(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New(name + " cannot be blank")
	}

	return nil
}
