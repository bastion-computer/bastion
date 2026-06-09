//go:build darwin

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

func newStartCommand(_ *rootOptions) *cobra.Command {
	addr := config.EnvDefault("BASTION_ADDR", config.DefaultAddr)
	bastiondSocket := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTION_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Bastion host API service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.ErrOrStderr(), "bastion start is not supported on macOS; use --api-url to connect to a remote Bastion host API")

			return err
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "host API listen address")
	cmd.Flags().StringVar(&bastiondSocket, "bastiond-socket", bastiondSocket, "bastiond Unix socket path")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}
