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
	namespaceID  string
	namespaceKey string
	clientConfig rootClientConfig
}

type rootClientConfig struct {
	DataDir      string          `json:"dataDir"`
	APIURL       rootOptionValue `json:"apiUrl"`
	NamespaceID  rootOptionValue `json:"namespaceId"`
	NamespaceKey rootOptionValue `json:"namespaceKey"`
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

	rootFlagAPIURL       = "api-url"
	rootFlagDataDir      = "data-dir"
	rootFlagNamespaceID  = "namespace-id"
	rootFlagNamespaceKey = "namespace-key"
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
		apiURL:       config.EnvDefault("BASTION_API_URL", config.DefaultAPIURL),
		dataDir:      config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir()),
		namespaceID:  os.Getenv("BASTION_NAMESPACE_ID"),
		namespaceKey: os.Getenv("BASTION_NAMESPACE_KEY"),
	}
	cmd := &cobra.Command{
		Use:           "bastion",
		Short:         "Self-hosted virtual machines for background coding agents.",
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
	cmd.PersistentFlags().StringVar(&opts.namespaceID, rootFlagNamespaceID, opts.namespaceID, "cluster namespace ID for resource commands")
	cmd.PersistentFlags().StringVar(&opts.namespaceKey, rootFlagNamespaceKey, opts.namespaceKey, "cluster namespace key for resource commands")
	cmd.AddCommand(
		newBaseCommand(opts),
		newStartCommand(opts),
		newSystemCommand(opts),
		newClientCommand(opts),
		newClusterCommand(opts),
		newSecretsCommand(opts),
		newUtilizationCommand(opts),
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
	case baseUse, clusterUse, secretsUse, utilizationUse, "templates", environmentUse, "mux", "opencode", proxyUse, "ssh":
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

	clientConfig, err := config.LoadClientConfig(dataDir)
	if err != nil {
		return rootClientConfig{}, err
	}

	apiURL := defaultAPIURL(cmd)
	source := rootOptionSourceDefault

	switch {
	case rootPersistentFlagChanged(cmd, rootFlagAPIURL):
		apiURL = opts.apiURL
		source = rootOptionSourceFlag
	case os.Getenv("BASTION_API_URL") != "":
		apiURL = os.Getenv("BASTION_API_URL")
		source = rootOptionSourceEnvironment
	default:
		if clientConfig.APIURL != "" {
			if err := validateClientAPIURL(clientConfig.APIURL); err != nil {
				return rootClientConfig{}, fmt.Errorf("client config api-url: %w", err)
			}

			apiURL = clientConfig.APIURL
			source = rootOptionSourceConfig
		}
	}

	namespaceID, namespaceIDSource, namespaceKey, namespaceKeySource, err := resolveNamespaceConfig(cmd, opts, clientConfig)
	if err != nil {
		return rootClientConfig{}, err
	}

	opts.apiURL = apiURL
	opts.namespaceID = namespaceID
	opts.namespaceKey = namespaceKey

	return rootClientConfig{
		DataDir: dataDir,
		APIURL: rootOptionValue{
			Value:  apiURL,
			Source: source,
		},
		NamespaceID: rootOptionValue{
			Value:  namespaceID,
			Source: namespaceIDSource,
		},
		NamespaceKey: rootOptionValue{
			Value:  namespaceKey,
			Source: namespaceKeySource,
		},
	}, nil
}

func resolveNamespaceConfig(cmd *cobra.Command, opts *rootOptions, clientConfig config.ClientConfig) (string, string, string, string, error) {
	flagIDChanged := rootPersistentFlagChanged(cmd, rootFlagNamespaceID)
	flagKeyChanged := rootPersistentFlagChanged(cmd, rootFlagNamespaceKey)
	envID := os.Getenv("BASTION_NAMESPACE_ID")
	envKey := os.Getenv("BASTION_NAMESPACE_KEY")

	var id, key, source string

	switch {
	case flagIDChanged || flagKeyChanged:
		if flagIDChanged {
			id = opts.namespaceID
		}

		if flagKeyChanged {
			key = opts.namespaceKey
		}

		source = rootOptionSourceFlag
	case envID != "" || envKey != "":
		id = envID
		key = envKey
		source = rootOptionSourceEnvironment
	case clientConfig.NamespaceID != "" || clientConfig.NamespaceKey != "":
		id = clientConfig.NamespaceID
		key = clientConfig.NamespaceKey
		source = rootOptionSourceConfig
	default:
		return "", rootOptionSourceDefault, "", rootOptionSourceDefault, nil
	}

	if id != "" && key != "" {
		return "", "", "", "", fmt.Errorf("specify only one of %s or %s", rootFlagNamespaceID, rootFlagNamespaceKey)
	}

	idSource := rootOptionSourceDefault
	if id != "" {
		idSource = source
	}

	keySource := rootOptionSourceDefault
	if key != "" {
		keySource = source
	}

	return id, idSource, key, keySource, nil
}

func defaultAPIURL(cmd *cobra.Command) string {
	if topLevel := topLevelCommand(cmd); topLevel != nil && topLevel.Name() == clusterUse {
		return config.DefaultClusterAPIURL
	}

	return config.DefaultAPIURL
}

func rootPersistentFlagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Root().PersistentFlags().Lookup(name)

	return flag != nil && flag.Changed
}
