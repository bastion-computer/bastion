// Package main contains the privileged Bastion daemon entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/config"
	fc "github.com/bastion-computer/bastion/core/internal/firecracker"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := newCommand()
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		return 1
	}

	return 0
}

func newCommand() *cobra.Command {
	socketUID := envInt("SUDO_UID", os.Getuid())
	socketGID := envInt("SUDO_GID", os.Getgid())
	vmUID := envInt("BASTIOND_VM_UID", 0)
	vmGID := envInt("BASTIOND_VM_GID", 0)
	dataDir := config.EnvDefault("BASTION_DATA_DIR", defaultDataDir())
	socketPath := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTIOND_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTIOND_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:           "bastiond",
		Short:         "Run the privileged Bastion Firecracker daemon",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() != 0 {
				return errors.New("bastiond must be run as root, for example: sudo bastiond")
			}

			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			resolvedDataDir, err := config.ExpandPath(dataDir)
			if err != nil {
				return err
			}

			logger.InfoContext(cmd.Context(), "bastiond listening",
				slog.String("socket", socketPath),
				slog.String("data_dir", resolvedDataDir),
				slog.Int("socket_uid", socketUID),
				slog.Int("socket_gid", socketGID),
				slog.Int("vm_uid", vmUID),
				slog.Int("vm_gid", vmGID),
				slog.String("log_format", logFormat),
				slog.String("log_level", logLevel),
			)

			manager := fc.NewManager(resolvedDataDir, vmUID, vmGID, logger)

			return fc.RunServer(cmd.Context(), fc.ServerOptions{
				SocketPath: socketPath,
				SocketUID:  socketUID,
				SocketGID:  socketGID,
				Manager:    manager,
				Logger:     logger,
			})
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", dataDir, "directory for persistent data")
	cmd.Flags().StringVar(&socketPath, "socket", socketPath, "Unix socket path")
	cmd.Flags().IntVar(&socketUID, "socket-uid", socketUID, "UID that owns the bastiond Unix socket")
	cmd.Flags().IntVar(&socketGID, "socket-gid", socketGID, "GID that owns the bastiond Unix socket")
	cmd.Flags().IntVar(&vmUID, "vm-uid", vmUID, "UID used by jailer for Firecracker")
	cmd.Flags().IntVar(&vmGID, "vm-gid", vmGID, "GID used by jailer for Firecracker")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func defaultDataDir() string {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.HomeDir + "/.bastion"
		}
	}

	return config.DefaultDataDir()
}
