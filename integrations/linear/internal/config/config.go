// Package config contains runtime configuration for the Linear integration.
//
//nolint:wsl_v5 // Config loading intentionally groups flag/env normalization steps.
package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is injected by release builds.
var Version = "dev"

// Config describes the Linear sidecar configuration.
type Config struct {
	Addr              string
	DataDir           string
	DatabasePath      string
	BastionAPIURL     string
	LinearAPIURL      string
	LinearToken       string
	WebhookSecret     string
	AppUserID         string
	EnvironmentTags   []string
	EnvironmentIDs    []string
	EnvironmentKeys   []string
	WorkerInterval    time.Duration
	OpenCodePort      int
	OpenCodeDirectory string
	OpenCodeAgent     string
	OpenCodeProvider  string
	OpenCodeModel     string
}

// Load returns configuration from environment variables and CLI flags.
func Load(args []string) (Config, error) {
	defaults, err := defaults()
	if err != nil {
		return Config{}, err
	}

	cfg := defaults
	flags := flag.NewFlagSet("bastion-linear", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "persistent data directory")
	flags.StringVar(&cfg.DatabasePath, "db", cfg.DatabasePath, "SQLite database path")
	flags.StringVar(&cfg.BastionAPIURL, "bastion-api-url", cfg.BastionAPIURL, "Bastion host API URL")
	flags.StringVar(&cfg.LinearAPIURL, "linear-api-url", cfg.LinearAPIURL, "Linear GraphQL API URL")
	flags.StringVar(&cfg.LinearToken, "linear-token", cfg.LinearToken, "Linear app actor API token")
	flags.StringVar(&cfg.WebhookSecret, "webhook-secret", cfg.WebhookSecret, "Linear webhook signing secret")
	flags.StringVar(&cfg.AppUserID, "app-user-id", cfg.AppUserID, "Linear app user ID used as issue delegate")
	flags.StringVar(&cfg.OpenCodeDirectory, "opencode-directory", cfg.OpenCodeDirectory, "OpenCode project directory inside environments")
	flags.StringVar(&cfg.OpenCodeAgent, "opencode-agent", cfg.OpenCodeAgent, "OpenCode agent name")
	flags.StringVar(&cfg.OpenCodeProvider, "opencode-provider", cfg.OpenCodeProvider, "OpenCode provider ID")
	flags.StringVar(&cfg.OpenCodeModel, "opencode-model", cfg.OpenCodeModel, "OpenCode model ID")
	flags.IntVar(&cfg.OpenCodePort, "opencode-port", cfg.OpenCodePort, "OpenCode server port inside environments")
	flags.DurationVar(&cfg.WorkerInterval, "worker-interval", cfg.WorkerInterval, "worker polling interval")

	var tags, ids, keys string
	tags = strings.Join(cfg.EnvironmentTags, ",")
	ids = strings.Join(cfg.EnvironmentIDs, ",")
	keys = strings.Join(cfg.EnvironmentKeys, ",")
	flags.StringVar(&tags, "environment-tags", tags, "comma-separated environment tags to target")
	flags.StringVar(&ids, "environment-ids", ids, "comma-separated environment ID glob patterns to target")
	flags.StringVar(&keys, "environment-keys", keys, "comma-separated environment key glob patterns to target")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}

	cfg.EnvironmentTags = splitList(tags)
	cfg.EnvironmentIDs = splitList(ids)
	cfg.EnvironmentKeys = splitList(keys)
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.DataDir, "sqlite.db")
	}

	return cfg, cfg.Validate()
}

// Validate checks required settings.
func (c Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, errors.New("addr is required"))
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		errs = append(errs, errors.New("db is required"))
	}
	if strings.TrimSpace(c.BastionAPIURL) == "" {
		errs = append(errs, errors.New("bastion API URL is required"))
	}
	if strings.TrimSpace(c.LinearAPIURL) == "" {
		errs = append(errs, errors.New("linear API URL is required"))
	}
	if strings.TrimSpace(c.LinearToken) == "" {
		errs = append(errs, errors.New("linear token is required"))
	}
	if strings.TrimSpace(c.WebhookSecret) == "" {
		errs = append(errs, errors.New("webhook secret is required"))
	}
	if c.OpenCodePort <= 0 || c.OpenCodePort > 65535 {
		errs = append(errs, errors.New("opencode port must be between 1 and 65535"))
	}
	if c.WorkerInterval <= 0 {
		errs = append(errs, errors.New("worker interval must be positive"))
	}

	return errors.Join(errs...)
}

func defaults() (Config, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Addr:              env("BASTION_LINEAR_ADDR", "localhost:3150"),
		DataDir:           env("BASTION_LINEAR_DATA_DIR", dataDir),
		DatabasePath:      os.Getenv("BASTION_LINEAR_DB"),
		BastionAPIURL:     env("BASTION_API_URL", "http://localhost:3148"),
		LinearAPIURL:      env("LINEAR_API_URL", "https://api.linear.app/graphql"),
		LinearToken:       os.Getenv("LINEAR_API_TOKEN"),
		WebhookSecret:     os.Getenv("LINEAR_WEBHOOK_SECRET"),
		AppUserID:         os.Getenv("LINEAR_APP_USER_ID"),
		EnvironmentTags:   splitList(os.Getenv("BASTION_LINEAR_ENVIRONMENT_TAGS")),
		EnvironmentIDs:    splitList(os.Getenv("BASTION_LINEAR_ENVIRONMENT_IDS")),
		EnvironmentKeys:   splitList(os.Getenv("BASTION_LINEAR_ENVIRONMENT_KEYS")),
		WorkerInterval:    envDuration("BASTION_LINEAR_WORKER_INTERVAL", 5*time.Second),
		OpenCodePort:      envInt("BASTION_LINEAR_OPENCODE_PORT", 4096),
		OpenCodeDirectory: env("BASTION_LINEAR_OPENCODE_DIRECTORY", ""),
		OpenCodeAgent:     os.Getenv("BASTION_LINEAR_OPENCODE_AGENT"),
		OpenCodeProvider:  os.Getenv("BASTION_LINEAR_OPENCODE_PROVIDER"),
		OpenCodeModel:     os.Getenv("BASTION_LINEAR_OPENCODE_MODEL"),
	}, nil
}

func defaultDataDir() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".bastion-linear"), nil
	}

	return "", errors.New("home is required when BASTION_LINEAR_DATA_DIR is not set")
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
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

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}
