//go:build !darwin

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	presetactions "github.com/bastion-computer/bastion/core/actions"
	"github.com/bastion-computer/bastion/core/internal/api"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

const (
	dataDirWaitTimeout  = 30 * time.Second
	dataDirWaitInterval = 100 * time.Millisecond
)

func newStartCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   startUse,
		Short: "Start a Bastion process",
	}
	cmd.AddCommand(
		newStartAPICommand(opts),
		newStartClusterCommand(opts),
		newStartDaemonCommand(opts),
	)

	return cmd
}

func newStartAPICommand(opts *rootOptions) *cobra.Command {
	addr := config.EnvDefault("BASTION_ADDR", config.DefaultAddr)
	bastiondSocket := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTION_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTION_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startAPIUse,
		Short: "Start the Bastion host API service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			resolvedDataDir, err := config.ExpandPath(opts.dataDir)
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
				api.WithDataDir(resolvedDataDir),
				api.WithBaseOrchestrator(daemonClient),
				api.WithTemplateOrchestrator(daemonClient),
				api.WithEnvironmentOrchestrator(daemonClient),
			)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", addr, "host API listen address")
	cmd.Flags().StringVar(&bastiondSocket, "bastiond-socket", bastiondSocket, "daemon Unix socket path")
	cmd.Flags().StringVar(&logFormat, "log-format", logFormat, "log format: json or text")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "minimum log level: debug, info, warn, or error")

	return cmd
}

func newStartDaemonCommand(opts *rootOptions) *cobra.Command {
	socketUID := envInt("SUDO_UID", os.Getuid())
	socketGID := envInt("SUDO_GID", os.Getgid())
	vmUID := envInt("BASTIOND_VM_UID", 0)
	vmGID := envInt("BASTIOND_VM_GID", 0)
	socketPath := config.EnvDefault("BASTIOND_SOCKET", config.DefaultBastiondSocket)
	logFormat := config.EnvDefault("BASTIOND_LOG_FORMAT", logging.DefaultFormat)
	logLevel := config.EnvDefault("BASTIOND_LOG_LEVEL", logging.DefaultLevel)

	cmd := &cobra.Command{
		Use:   startDaemonUse,
		Short: "Start the privileged Bastion Cloud Hypervisor daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Geteuid() != 0 {
				return errors.New("bastion start daemon must be run as root, for example: sudo bastion start daemon")
			}

			logger, err := logging.New(cmd.ErrOrStderr(), logFormat, logLevel)
			if err != nil {
				return err
			}

			dataDir := opts.dataDir
			if !rootPersistentFlagChanged(cmd, rootFlagDataDir) && os.Getenv("BASTION_DATA_DIR") == "" {
				dataDir = defaultDaemonDataDir()
			}

			resolvedDataDir, err := config.ExpandPath(dataDir)
			if err != nil {
				return err
			}

			if err := waitForDataDir(cmd.Context(), resolvedDataDir, dataDirWaitTimeout, dataDirWaitInterval); err != nil {
				return err
			}

			if err := presetactions.Seed(resolvedDataDir); err != nil {
				return err
			}

			logger.InfoContext(cmd.Context(), "bastion daemon listening",
				slog.String("socket", socketPath),
				slog.String("data_dir", resolvedDataDir),
				slog.Int("socket_uid", socketUID),
				slog.Int("socket_gid", socketGID),
				slog.Int("vm_uid", vmUID),
				slog.Int("vm_gid", vmGID),
				slog.String("log_format", logFormat),
				slog.String("log_level", logLevel),
			)

			manager := ch.NewManager(resolvedDataDir, vmUID, vmGID, logger)
			manager.ProxyUID = socketUID
			manager.ProxyGID = socketGID

			return ch.RunServer(cmd.Context(), ch.ServerOptions{
				SocketPath: socketPath,
				SocketUID:  socketUID,
				SocketGID:  socketGID,
				Manager:    manager,
				Logger:     logger,
			})
		},
	}
	cmd.Flags().StringVar(&socketPath, "socket", socketPath, "Unix socket path")
	cmd.Flags().IntVar(&socketUID, "socket-uid", socketUID, "UID that owns the Bastion daemon Unix socket")
	cmd.Flags().IntVar(&socketGID, "socket-gid", socketGID, "GID that owns the Bastion daemon Unix socket")
	cmd.Flags().IntVar(&vmUID, "vm-uid", vmUID, "UID used for VM-owned runtime files")
	cmd.Flags().IntVar(&vmGID, "vm-gid", vmGID, "GID used for VM-owned runtime files")
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

func defaultDaemonDataDir() string {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.HomeDir + "/.bastion"
		}
	}

	return config.DefaultDataDir()
}

func waitForDataDir(ctx context.Context, dataDir string, timeout, interval time.Duration) error {
	if dataDir == "" {
		return errors.New("data dir is required")
	}

	if interval <= 0 {
		interval = dataDirWaitInterval
	}

	if err := dataDirReady(dataDir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("timed out waiting %s for API to create data directory %s", timeout, dataDir)
		case <-ticker.C:
			if err := dataDirReady(dataDir); err == nil {
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
}

func dataDirReady(dataDir string) error {
	info, err := os.Stat(dataDir)
	if err != nil {
		return fmt.Errorf("stat data directory %s: %w", dataDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("data directory %s is not a directory", dataDir)
	}

	return nil
}
