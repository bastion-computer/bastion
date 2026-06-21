// Package config resolves Bastion runtime configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultAddr is the local Bastion API listen address.
const DefaultAddr = "localhost:3148"

// DefaultAPIURL is the local Bastion API endpoint used by the CLI.
const DefaultAPIURL = "http://" + DefaultAddr

// DefaultClusterAddr is the local Bastion cluster API listen address.
const DefaultClusterAddr = "localhost:3150"

// DefaultClusterAPIURL is the local Bastion cluster API endpoint used by cluster CLI commands.
const DefaultClusterAPIURL = "http://" + DefaultClusterAddr

// DefaultClusterDatabaseURL is the local development Postgres database for the cluster control plane.
const DefaultClusterDatabaseURL = "postgres://bastion:bastion@localhost:3151/bastion_cluster?sslmode=disable" //nolint:gosec // Local development-only Postgres credentials.

// DefaultBastiondSocket is the Unix socket used by the privileged daemon.
const DefaultBastiondSocket = "/run/bastion/bastiond.sock"

// Version is the Bastion CLI version.
var Version = "dev"

// EnvDefault returns an environment variable value or fallback when unset.
func EnvDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

// DefaultDataDir returns the default Bastion data directory.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".bastion"
	}

	return filepath.Join(home, ".bastion")
}

// ExpandPath expands a user path and returns an absolute path.
func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}

	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}

		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	return abs, nil
}
