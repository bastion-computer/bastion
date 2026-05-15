package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
)

type rootOptions struct {
	apiURL string
}

// Execute runs the Bastion root command.
func Execute(ctx context.Context) error {
	cmd := NewRootCommand()
	cmd.SetContext(ctx)

	return cmd.Execute()
}

// NewRootCommand builds the Bastion root command tree.
func NewRootCommand() *cobra.Command {
	opts := &rootOptions{
		apiURL: config.EnvDefault("BASTION_API_URL", config.DefaultAPIURL),
	}
	cmd := &cobra.Command{
		Use:           "bastion",
		Short:         "An orchestration platform to run many opencode agents in isolated and reproducible dev environments.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.apiURL, "api-url", opts.apiURL, "host API URL")
	cmd.AddCommand(
		newStartCommand(),
		newTemplatesCommand(opts),
		newEnvironmentCommand(opts),
		newVersionCommand(),
	)

	return cmd
}
