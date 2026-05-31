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
	"github.com/bastion-computer/bastion/core/internal/handlers/queues"
	"github.com/bastion-computer/bastion/core/internal/handlers/templates"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
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
	environmentOrchestrator environment.Orchestrator
	environmentSSHRunner    environments.SSHRunner
	queueProxy              environment.QueueProxy
}

// RouterOption configures the Bastion API router.
type RouterOption func(*routerConfig)

// WithEnvironmentOrchestrator configures the VM orchestrator for environment routes.
func WithEnvironmentOrchestrator(orchestrator environment.Orchestrator) RouterOption {
	return func(cfg *routerConfig) {
		cfg.environmentOrchestrator = orchestrator
	}
}

// WithEnvironmentSSHRunner configures the environment SSH stream runner.
func WithEnvironmentSSHRunner(runner environments.SSHRunner) RouterOption {
	return func(cfg *routerConfig) {
		cfg.environmentSSHRunner = runner
	}
}

// WithQueueProxy configures the per-environment queue proxy lifecycle hooks.
func WithQueueProxy(proxy environment.QueueProxy) RouterOption {
	return func(cfg *routerConfig) {
		cfg.queueProxy = proxy
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

	queueService := queue.NewService(db)

	templateHandler := templates.NewHandler(template.NewService(db))
	templateRoutes := v1.Group("/templates")
	templateRoutes.POST("", templateHandler.Create)
	templateRoutes.GET("", templateHandler.List)
	templateRoutes.GET("/:id", templateHandler.GetByID)
	templateRoutes.GET("/by-key/:key", templateHandler.GetByKey)
	templateRoutes.DELETE("/:id", templateHandler.RemoveByID)
	templateRoutes.DELETE("/by-key/:key", templateHandler.RemoveByKey)

	environmentHandler := environments.NewHandler(
		environment.NewService(db, environment.WithOrchestrator(cfg.environmentOrchestrator), environment.WithQueueService(queueService), environment.WithQueueProxy(cfg.queueProxy)),
		environments.WithSSHRunner(cfg.environmentSSHRunner),
	)
	environmentRoutes := v1.Group("/environments")
	environmentRoutes.POST("", environmentHandler.Create)
	environmentRoutes.GET("", environmentHandler.List)
	environmentRoutes.GET("/by-key/:key", environmentHandler.GetByKey)
	environmentRoutes.DELETE("/by-key/:key", environmentHandler.RemoveByKey)
	environmentRoutes.POST("/:id/ssh", environmentHandler.SSH)
	environmentRoutes.GET("/:id", environmentHandler.Get)
	environmentRoutes.DELETE("/:id", environmentHandler.Remove)

	queueHandler := queues.NewHandler(queueService)
	queueRoutes := v1.Group("/queues")
	queueRoutes.POST("", queueHandler.Create)
	queueRoutes.GET("", queueHandler.List)
	queueRoutes.GET("/by-key/:key", queueHandler.GetByKey)
	queueRoutes.DELETE("/by-key/:key", queueHandler.RemoveByKey)
	queueRoutes.POST("/by-key/:key/tasks", queueHandler.PublishByKey)
	queueRoutes.GET("/by-key/:key/tasks/:taskID", queueHandler.GetTaskByKey)
	queueRoutes.POST("/by-key/:key/lease", queueHandler.LeaseByKey)
	queueRoutes.POST("/by-key/:key/tasks/:taskID/ack", queueHandler.AckByKey)
	queueRoutes.POST("/by-key/:key/tasks/:taskID/fail", queueHandler.FailByKey)
	queueRoutes.GET("/:id", queueHandler.GetByID)
	queueRoutes.DELETE("/:id", queueHandler.RemoveByID)
	queueRoutes.POST("/:id/tasks", queueHandler.PublishByID)
	queueRoutes.GET("/:id/tasks/:taskID", queueHandler.GetTaskByID)
	queueRoutes.POST("/:id/lease", queueHandler.LeaseByID)
	queueRoutes.POST("/:id/tasks/:taskID/ack", queueHandler.AckByID)
	queueRoutes.POST("/:id/tasks/:taskID/fail", queueHandler.FailByID)

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
