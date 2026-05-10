package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/api"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/database"
)

func newStartCommand() *cobra.Command {
	addr := config.EnvDefault("BASTION_ADDR", "localhost:3148")
	dataDir := config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir())

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Bastion host API service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedDataDir, err := config.ExpandPath(dataDir)
			if err != nil {
				return err
			}

			db, err := database.Open(resolvedDataDir)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "bastion host API listening on http://%s\n", addr); err != nil {
				return fmt.Errorf("write startup message: %w", err)
			}

			return api.Run(cmd.Context(), addr, db)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "host API listen address")
	cmd.Flags().StringVar(&dataDir, "data-dir", dataDir, "directory for persistent data")

	return cmd
}
