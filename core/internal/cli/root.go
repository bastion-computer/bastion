package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
)

type rootOptions struct {
	apiURL       string
	dataDir      string
	clientConfig rootClientConfig
}

type rootClientConfig struct {
	DataDir string          `json:"dataDir"`
	APIURL  rootOptionValue `json:"apiUrl"`
}

type rootOptionValue struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

const (
	rootOptionSourceDefault     = "default"
	rootOptionSourceEnvironment = "environment"
	rootOptionSourceFlag        = "flag"
	rootOptionSourceConfig      = "config"

	rootFlagAPIURL  = "api-url"
	rootFlagDataDir = "data-dir"
)

// Execute runs the Bastion root command.
func Execute(ctx context.Context) error {
	cmd := NewRootCommand()
	cmd.SetContext(ctx)

	return cmd.Execute()
}

// NewRootCommand builds the Bastion root command tree.
func NewRootCommand() *cobra.Command {
	opts := &rootOptions{
		apiURL:  config.EnvDefault("BASTION_API_URL", config.DefaultAPIURL),
		dataDir: config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir()),
	}
	cmd := &cobra.Command{
		Use:           "bastion",
		Short:         "Bastion deploys virtual computers for coding agents.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !shouldResolveClientConfig(cmd) {
				return nil
			}

			resolved, err := resolveClientConfig(cmd, opts)
			if err != nil {
				return err
			}

			opts.clientConfig = resolved

			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&opts.apiURL, rootFlagAPIURL, opts.apiURL, "host API URL")
	cmd.PersistentFlags().StringVar(&opts.dataDir, rootFlagDataDir, opts.dataDir, "directory for persistent data")
	cmd.AddCommand(
		newStartCommand(opts),
		newSystemCommand(opts),
		newClientCommand(opts),
		newSecretsCommand(opts),
		newTemplatesCommand(opts),
		newEnvironmentCommand(opts),
		newMuxCommand(opts),
		newOpenCodeCommand(opts),
		newProxyCommand(opts),
		newSSHCommand(opts),
		newVersionCommand(),
	)

	return cmd
}

func shouldResolveClientConfig(cmd *cobra.Command) bool {
	topLevel := topLevelCommand(cmd)
	if topLevel == nil {
		return false
	}

	switch topLevel.Name() {
	case secretsUse, "templates", environmentUse, "mux", "opencode", proxyUse, "ssh":
		return true
	case clientUse:
		return cmd.Name() == rootOptionSourceConfig
	default:
		return false
	}
}

func topLevelCommand(cmd *cobra.Command) *cobra.Command {
	for cmd != nil && cmd.Parent() != nil && cmd.Parent().Parent() != nil {
		cmd = cmd.Parent()
	}

	return cmd
}

func resolveClientConfig(cmd *cobra.Command, opts *rootOptions) (rootClientConfig, error) {
	dataDir, err := config.ExpandPath(opts.dataDir)
	if err != nil {
		return rootClientConfig{}, err
	}

	apiURL := config.DefaultAPIURL
	source := rootOptionSourceDefault

	switch {
	case rootPersistentFlagChanged(cmd, rootFlagAPIURL):
		apiURL = opts.apiURL
		source = rootOptionSourceFlag
	case os.Getenv("BASTION_API_URL") != "":
		apiURL = os.Getenv("BASTION_API_URL")
		source = rootOptionSourceEnvironment
	default:
		clientConfig, err := config.LoadClientConfig(dataDir)
		if err != nil {
			return rootClientConfig{}, err
		}

		if clientConfig.APIURL != "" {
			if err := validateClientAPIURL(clientConfig.APIURL); err != nil {
				return rootClientConfig{}, fmt.Errorf("client config api-url: %w", err)
			}

			apiURL = clientConfig.APIURL
			source = rootOptionSourceConfig
		}
	}

	opts.apiURL = apiURL

	return rootClientConfig{
		DataDir: dataDir,
		APIURL: rootOptionValue{
			Value:  apiURL,
			Source: source,
		},
	}, nil
}

func rootPersistentFlagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Root().PersistentFlags().Lookup(name)

	return flag != nil && flag.Changed
}
