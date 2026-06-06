// Package main is the Linear integration entrypoint.
//
//nolint:wsl_v5 // Entrypoint setup reads clearer when grouped by subsystem.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bastion-computer/bastion/integrations/linear/internal/api"
	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
	"github.com/bastion-computer/bastion/integrations/linear/internal/config"
	"github.com/bastion-computer/bastion/integrations/linear/internal/database"
	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
	"github.com/bastion-computer/bastion/integrations/linear/internal/opencode"
	"github.com/bastion-computer/bastion/integrations/linear/internal/service"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Println(config.Version)
		return 0
	}

	cfg, err := config.Load(args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", slog.String("error", err.Error()))
		return 1
	}
	defer func() { _ = db.Close() }()

	bastionClient := bastion.NewClient(cfg.BastionAPIURL)
	linearClient := linear.NewClient(cfg.LinearAPIURL, cfg.LinearToken)
	opencodeClient := opencode.NewClient(bastionClient, opencode.Config{
		Port:      cfg.OpenCodePort,
		Directory: cfg.OpenCodeDirectory,
		Agent:     cfg.OpenCodeAgent,
		Provider:  cfg.OpenCodeProvider,
		Model:     cfg.OpenCodeModel,
	})
	svc := service.New(db, linearClient, bastionClient, opencodeClient, service.Config{
		Selector: service.Selector{
			Tags:        cfg.EnvironmentTags,
			IDPatterns:  cfg.EnvironmentIDs,
			KeyPatterns: cfg.EnvironmentKeys,
		},
		AppUserID:      cfg.AppUserID,
		WorkerInterval: cfg.WorkerInterval,
	}, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc.Start(ctx)

	logger.Info("starting Linear integration", slog.String("addr", cfg.Addr))
	if err := api.NewServer(cfg.Addr, cfg.WebhookSecret, svc, logger).Run(ctx); err != nil {
		logger.Error("Linear integration stopped", slog.String("error", err.Error()))
		return 1
	}

	return 0
}
