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
	"time"

	"github.com/spf13/cobra"

	presetactions "github.com/bastion-computer/bastion/core/actions"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/config"
	"github.com/bastion-computer/bastion/core/internal/logging"
)

const (
	dataDirWaitTimeout  = 30 * time.Second
	dataDirWaitInterval = 100 * time.Millisecond
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
		Short:         "Run the privileged Bastion Cloud Hypervisor daemon",
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

			if err := waitForDataDir(cmd.Context(), resolvedDataDir, dataDirWaitTimeout, dataDirWaitInterval); err != nil {
				return err
			}

			if err := presetactions.Seed(resolvedDataDir); err != nil {
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
	cmd.Flags().StringVar(&dataDir, "data-dir", dataDir, "directory for persistent data")
	cmd.Flags().StringVar(&socketPath, "socket", socketPath, "Unix socket path")
	cmd.Flags().IntVar(&socketUID, "socket-uid", socketUID, "UID that owns the bastiond Unix socket")
	cmd.Flags().IntVar(&socketGID, "socket-gid", socketGID, "GID that owns the bastiond Unix socket")
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

func defaultDataDir() string {
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
