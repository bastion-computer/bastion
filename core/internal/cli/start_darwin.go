//go:build darwin

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

func newStartCommand(_ *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   startUse,
		Short: "Start a Bastion process",
	}
	cmd.AddCommand(
		newUnsupportedStartAPICommand(),
		newUnsupportedStartDaemonCommand(),
		newUnsupportedStartClusterCommand(),
	)

	return cmd
}

func newUnsupportedStartAPICommand() *cobra.Command {
	addr := config.EnvDefault("BASTION_ADDR", config.DefaultAddr)
	bastiondSocket := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTION_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startAPIUse,
		Short: "Start the Bastion host API service",
		Args:  cobra.NoArgs,
		RunE:  unsupportedStartProcess,
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "host API listen address")
	cmd.Flags().StringVar(&bastiondSocket, "bastiond-socket", bastiondSocket, "daemon Unix socket path")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func newUnsupportedStartDaemonCommand() *cobra.Command {
	socketPath := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTIOND_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTIOND_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startDaemonUse,
		Short: "Start the privileged Bastion Cloud Hypervisor daemon",
		Args:  cobra.NoArgs,
		RunE:  unsupportedStartProcess,
	}
	cmd.Flags().StringVar(&socketPath, "socket", socketPath, "Unix socket path")
	cmd.Flags().Int("socket-uid", 0, "UID that owns the Bastion daemon Unix socket")
	cmd.Flags().Int("socket-gid", 0, "GID that owns the Bastion daemon Unix socket")
	cmd.Flags().Int("vm-uid", 0, "UID used for VM-owned runtime files")
	cmd.Flags().Int("vm-gid", 0, "GID used for VM-owned runtime files")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func newUnsupportedStartClusterCommand() *cobra.Command {
	addr := config.EnvDefault("BASTION_CLUSTER_ADDR", config.DefaultClusterAddr)
	databaseURL := config.EnvDefault("BASTION_CLUSTER_DATABASE_URL", "")
	logFormat := config.EnvDefault("BASTION_CLUSTER_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTION_CLUSTER_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startClusterUse,
		Short: "Start the Bastion cluster control plane",
		Args:  cobra.NoArgs,
		RunE:  unsupportedStartProcess,
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "cluster API listen address")
	cmd.Flags().StringVar(&databaseURL, "database-url", databaseURL, "Postgres database URL for cluster state")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func unsupportedStartProcess(cmd *cobra.Command, _ []string) error {
	_, err := fmt.Fprintln(cmd.ErrOrStderr(), "bastion start is not supported on macOS; use --api-url to connect to a remote Bastion host API")

	return err
}
