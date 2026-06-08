// Package api exposes the local Bastion HTTP API.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/handlers/environments"
	"github.com/bastion-computer/bastion/core/internal/handlers/templates"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// NewServer returns an HTTP server configured for the Bastion API.
func NewServer(addr string, db *database.Client, logger *slog.Logger, opts ...RouterOption) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(db, logger, opts...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

type routerConfig struct {
	templateOrchestrator    template.Orchestrator
	environmentOrchestrator environment.Orchestrator
	environmentSSHRunner    environments.SSHRunner
}

// RouterOption configures the Bastion API router.
type RouterOption func(*routerConfig)

// WithEnvironmentOrchestrator configures the VM orchestrator for environment routes.
func WithEnvironmentOrchestrator(orchestrator environment.Orchestrator) RouterOption {
	return func(cfg *routerConfig) {
		cfg.environmentOrchestrator = orchestrator
	}
}

// WithTemplateOrchestrator configures VM preparation for template routes.
func WithTemplateOrchestrator(orchestrator template.Orchestrator) RouterOption {
	return func(cfg *routerConfig) {
		cfg.templateOrchestrator = orchestrator
	}
}

// WithEnvironmentSSHRunner configures the environment SSH stream runner.
func WithEnvironmentSSHRunner(runner environments.SSHRunner) RouterOption {
	return func(cfg *routerConfig) {
		cfg.environmentSSHRunner = runner
	}
}

// NewRouter builds the Bastion API router.
func NewRouter(db *database.Client, logger *slog.Logger, opts ...RouterOption) *gin.Engine {
	cfg := routerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	router := gin.New()
	router.Use(requestIDMiddleware(), slogMiddleware(logger), recoveryMiddleware(logger))

	v1 := router.Group("/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	templateHandler := templates.NewHandler(template.NewService(db, template.WithOrchestrator(cfg.templateOrchestrator)))
	templateRoutes := v1.Group("/templates")
	templateRoutes.POST("", templateHandler.Create)
	templateRoutes.GET("", templateHandler.List)
	templateRoutes.GET("/:id", templateHandler.GetByID)
	templateRoutes.GET("/by-key/:key", templateHandler.GetByKey)
	templateRoutes.DELETE("/:id", templateHandler.RemoveByID)
	templateRoutes.DELETE("/by-key/:key", templateHandler.RemoveByKey)

	environmentHandler := environments.NewHandler(
		environment.NewService(db, environment.WithOrchestrator(cfg.environmentOrchestrator)),
		environments.WithSSHRunner(cfg.environmentSSHRunner),
	)
	environmentRoutes := v1.Group("/environments")
	environmentRoutes.POST("", environmentHandler.Create)
	environmentRoutes.GET("", environmentHandler.List)
	environmentRoutes.GET("/by-key/:key", environmentHandler.GetByKey)
	environmentRoutes.DELETE("/by-key/:key", environmentHandler.RemoveByKey)
	environmentRoutes.Any("/by-key/:key/agents/:agent", environmentHandler.AgentProxy)
	environmentRoutes.Any("/by-key/:key/agents/:agent/*path", environmentHandler.AgentProxy)
	environmentRoutes.POST("/:id/ssh", environmentHandler.SSH)
	environmentRoutes.Any("/:id/agents/:agent", environmentHandler.AgentProxy)
	environmentRoutes.Any("/:id/agents/:agent/*path", environmentHandler.AgentProxy)
	environmentRoutes.GET("/:id", environmentHandler.Get)
	environmentRoutes.DELETE("/:id", environmentHandler.Remove)

	return router
}

// Run starts the Bastion API server and shuts it down when ctx is cancelled.
func Run(ctx context.Context, addr string, db *database.Client, logger *slog.Logger, opts ...RouterOption) error {
	server := NewServer(addr, db, logger, opts...)
	errCh := make(chan error, 1)

	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}

		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.ErrorContext(ctx, "host API failed", slog.String("error", err.Error()))
		}

		return err
	case <-ctx.Done():
		logger.InfoContext(context.Background(), "host API shutting down", slog.String("addr", addr))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown host API: %w", err)
		}

		if err := <-errCh; err != nil {
			return err
		}

		logger.InfoContext(context.Background(), "host API stopped", slog.String("addr", addr))

		return nil
	}
}
