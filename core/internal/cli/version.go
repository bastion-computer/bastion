package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Bastion version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), config.Version)
			return err
		},
	}
}
