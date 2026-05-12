package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newExecCommand(opts *rootOptions) *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "exec --id SANDBOX_ID COMMAND [ARG...]",
		Short: "Run a command in a sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return errors.New("--id is required")
			}

			response, err := apiClient(opts).ExecSandbox(cmd.Context(), id, args)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), response)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "sandbox ID")
	cmd.Flags().SetInterspersed(false)

	return cmd
}
