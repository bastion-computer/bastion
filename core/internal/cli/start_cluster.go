package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/clusterapi"
	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

func newStartClusterCommand(_ *rootOptions) *cobra.Command {
	addr := config.EnvDefault("BASTION_CLUSTER_ADDR", config.DefaultClusterAddr)
	databaseURL := clusterDatabaseURLDefault()
	logFormat := clusterLogDefault("BASTION_CLUSTER_LOG_FORMAT", "BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := clusterLogDefault("BASTION_CLUSTER_LOG_LEVEL", "BASTION_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startClusterUse,
		Short: "Start the Bastion cluster control plane service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			db, err := clusterdb.Open(cmd.Context(), databaseURL)
			if err != nil {
				return err
			}
			defer db.Close()

			logger.InfoContext(cmd.Context(), "cluster API listening",
				slog.String("addr", addr),
				slog.String("database_url", databaseURL),
				slog.String("log_format", logFormat),
				slog.String("log_level", logLevel),
			)

			return clusterapi.Run(cmd.Context(), addr, db, logger)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "cluster API listen address")
	cmd.Flags().StringVar(&databaseURL, "database-url", databaseURL, "cluster Postgres database URL")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func clusterDatabaseURLDefault() string {
	if value := os.Getenv("BASTION_CLUSTER_DATABASE_URL"); value != "" {
		return value
	}

	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}

	return config.DefaultClusterDatabaseURL
}

func clusterLogDefault(primary, fallback, defaultValue string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}

	return config.EnvDefault(fallback, defaultValue)
}
