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
		Short:         "The open source platform to deploy, run, and scale AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.apiURL, "api-url", opts.apiURL, "host API URL")
	cmd.AddCommand(
		newStartCommand(),
		newSecretsCommand(opts),
		newTemplatesCommand(opts),
		newSandboxCommand(opts),
		newCheckpointsCommand(opts),
		newExecCommand(opts),
		newVersionCommand(),
	)

	return cmd
}

func addListFlags(cmd *cobra.Command, limit *int, cursor *string) {
	cmd.Flags().IntVar(limit, "limit", 20, "maximum entries to return")
	cmd.Flags().StringVar(cursor, "cursor", "", "pagination cursor")
}
