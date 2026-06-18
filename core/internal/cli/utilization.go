package cli

import "github.com/spf13/cobra"

func newUtilizationCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   utilizationUse,
		Short: "Show host utilization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			utilization, err := apiClient(opts).GetUtilization(cmd.Context())
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), utilization)
		},
	}
}
