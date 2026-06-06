package cli

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/api"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

func newStartCommand() *cobra.Command {
	addr := config.EnvDefault("BASTION_ADDR", config.DefaultAddr)
	dataDir := config.EnvDefault("BASTION_DATA_DIR", config.DefaultDataDir())
	bastiondSocket := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTION_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Bastion host API service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			resolvedDataDir, err := config.ExpandPath(dataDir)
			if err != nil {
				return err
			}

			db, err := database.Open(resolvedDataDir)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			logger.InfoContext(cmd.Context(), "host API listening",
				slog.String("addr", addr),
				slog.String("data_dir", resolvedDataDir),
				slog.String("bastiond_socket", bastiondSocket),
				slog.String("log_format", logFormat),
				slog.String("log_level", logLevel),
			)

			daemonClient := ch.NewClient(bastiondSocket)

			return api.Run(cmd.Context(), addr, db, logger,
				api.WithTemplateOrchestrator(daemonClient),
				api.WithEnvironmentOrchestrator(daemonClient),
			)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "host API listen address")
	cmd.Flags().StringVar(&dataDir, "data-dir", dataDir, "directory for persistent data")
	cmd.Flags().StringVar(&bastiondSocket, "bastiond-socket", bastiondSocket, "bastiond Unix socket path")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}
