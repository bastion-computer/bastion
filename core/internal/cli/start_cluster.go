package cli

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/clusterapi"
	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/logging"
	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
)

func newStartClusterCommand(_ *rootOptions) *cobra.Command {
	addr := config.EnvDefault("BASTION_CLUSTER_ADDR", config.DefaultClusterAddr)
	databaseURL := clusterDatabaseURLDefault()
	logFormat := clusterLogDefault("BASTION_CLUSTER_LOG_FORMAT", "BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := clusterLogDefault("BASTION_CLUSTER_LOG_LEVEL", "BASTION_LOG_LEVEL", logging.DefaultLevel)
	s3Bucket := config.EnvDefault("BASTION_CLUSTER_S3_BUCKET", "")
	s3Endpoint := config.EnvDefault("BASTION_CLUSTER_S3_ENDPOINT", "")
	s3Region := config.EnvDefault("BASTION_CLUSTER_S3_REGION", "us-east-1")
	s3AccessKeyID := clusterLogDefault("BASTION_CLUSTER_S3_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID", "")
	s3SecretAccessKey := clusterLogDefault("BASTION_CLUSTER_S3_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY", "")
	s3UsePathStyle := envBool("BASTION_CLUSTER_S3_USE_PATH_STYLE", false)

	cmd := &cobra.Command{
		Use:   startClusterUse,
		Short: "Start the Bastion cluster control plane service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			routerOptions := []clusterapi.RouterOption{}

			if s3Bucket != "" {
				archiveStore, err := clusterservice.NewS3TemplateArchiveStore(cmd.Context(), clusterservice.S3ArchiveStoreOptions{
					Bucket:          s3Bucket,
					Region:          s3Region,
					Endpoint:        s3Endpoint,
					AccessKeyID:     s3AccessKeyID,
					SecretAccessKey: s3SecretAccessKey,
					UsePathStyle:    s3UsePathStyle,
				})
				if err != nil {
					return err
				}

				routerOptions = append(routerOptions, clusterapi.WithTemplateArchiveStore(archiveStore))
			}

			db, err := clusterdb.Open(cmd.Context(), databaseURL)
			if err != nil {
				return err
			}
			defer db.Close()

			logger.InfoContext(cmd.Context(), "cluster API listening",
				slog.String("addr", addr),
				slog.String("database_url", databaseURL),
				slog.String("s3_bucket", s3Bucket),
				slog.String("s3_endpoint", s3Endpoint),
				slog.String("s3_region", s3Region),
				slog.String("log_format", logFormat),
				slog.String("log_level", logLevel),
			)

			return clusterapi.Run(cmd.Context(), addr, db, logger, routerOptions...)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "cluster API listen address")
	cmd.Flags().StringVar(&databaseURL, "database-url", databaseURL, "cluster Postgres database URL")
	cmd.Flags().StringVar(&s3Bucket, "s3-bucket", s3Bucket, "S3 bucket for cluster template archives")
	cmd.Flags().StringVar(&s3Endpoint, "s3-endpoint", s3Endpoint, "S3-compatible endpoint URL for cluster template archives")
	cmd.Flags().StringVar(&s3Region, "s3-region", s3Region, "S3 region for cluster template archives")
	cmd.Flags().StringVar(&s3AccessKeyID, "s3-access-key-id", s3AccessKeyID, "S3 access key ID for cluster template archives")
	cmd.Flags().StringVar(&s3SecretAccessKey, "s3-secret-access-key", s3SecretAccessKey, "S3 secret access key for cluster template archives")
	cmd.Flags().BoolVar(&s3UsePathStyle, "s3-use-path-style", s3UsePathStyle, "use path-style S3 URLs for cluster template archives")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
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
