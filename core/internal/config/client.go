package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const clientConfigFile = "client.json"

// ClientConfig contains persisted CLI client option overrides.
type ClientConfig struct {
	APIURL string `json:"apiUrl,omitempty"`
}

// ClientConfigPath returns the path to the persisted client config file.
func ClientConfigPath(dataDir string) (string, error) {
	resolvedDataDir, err := ExpandPath(dataDir)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolvedDataDir, clientConfigFile), nil
}

// LoadClientConfig reads persisted CLI client option overrides.
func LoadClientConfig(dataDir string) (ClientConfig, error) {
	var cfg ClientConfig

	path, err := ClientConfigPath(dataDir)
	if err != nil {
		return cfg, err
	}

	contents, err := os.ReadFile(path) //nolint:gosec // The config path is resolved from the user-selected Bastion data directory.
	if os.IsNotExist(err) {
		return cfg, nil
	}

	if err != nil {
		return cfg, fmt.Errorf("read client config: %w", err)
	}

	if err := json.Unmarshal(contents, &cfg); err != nil {
		return cfg, fmt.Errorf("decode client config: %w", err)
	}

	return cfg, nil
}

// SaveClientConfig writes persisted CLI client option overrides.
func SaveClientConfig(dataDir string, cfg ClientConfig) error {
	path, err := ClientConfigPath(dataDir)
	if err != nil {
		return err
	}

	if cfg.Empty() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove client config: %w", err)
		}

		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create client config directory: %w", err)
	}

	contents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode client config: %w", err)
	}

	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write client config: %w", err)
	}

	return nil
}

// Empty reports whether the client config contains no overrides.
func (cfg ClientConfig) Empty() bool {
	return cfg.APIURL == ""
}
