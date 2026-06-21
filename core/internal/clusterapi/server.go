// Package clusterapi exposes the Bastion cluster control plane HTTP API.
package clusterapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	clusterhandler "github.com/bastion-computer/bastion/core/internal/handlers/cluster"
	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// RouterOption configures the cluster API router.
type RouterOption func(*routerConfig)

type routerConfig struct {
	nodeClient   clusterservice.NodeClient
	archiveStore clusterservice.TemplateArchiveStore
}

// WithNodeClient configures how aggregate routes call underlying Bastion API nodes.
func WithNodeClient(client clusterservice.NodeClient) RouterOption {
	return func(cfg *routerConfig) {
		cfg.nodeClient = client
	}
}

// WithTemplateArchiveStore configures template archive storage for cluster resource routes.
func WithTemplateArchiveStore(store clusterservice.TemplateArchiveStore) RouterOption {
	return func(cfg *routerConfig) {
		cfg.archiveStore = store
	}
}

// NewServer returns an HTTP server configured for the Bastion cluster API.
func NewServer(addr string, db *clusterdb.Client, logger *slog.Logger, opts ...RouterOption) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(db, logger, opts...),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// NewRouter builds the Bastion cluster API router.
func NewRouter(db *clusterdb.Client, logger *slog.Logger, opts ...RouterOption) *gin.Engine {
	cfg := routerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	serviceOptions := []clusterservice.Option{}
	if cfg.nodeClient != nil {
		serviceOptions = append(serviceOptions, clusterservice.WithNodeClient(cfg.nodeClient))
	}

	if cfg.archiveStore != nil {
		serviceOptions = append(serviceOptions, clusterservice.WithTemplateArchiveStore(cfg.archiveStore))
	}

	router := gin.New()
	router.Use(requestIDMiddleware(), slogMiddleware(logger), recoveryMiddleware(logger))

	handler := clusterhandler.NewHandler(clusterservice.NewService(db, serviceOptions...))

	v1 := router.Group("/v1")
	v1.GET("/health", handler.Health)
	v1.GET("/utilization", handler.Utilization)

	secretRoutes := v1.Group("/secrets")
	secretRoutes.POST("", handler.CreateSecret)
	secretRoutes.GET("", handler.ListSecrets)
	secretRoutes.GET("/:id", handler.GetSecretByID)
	secretRoutes.GET("/by-key/:key", handler.GetSecretByKey)
	secretRoutes.DELETE("/:id", handler.RemoveSecretByID)
	secretRoutes.DELETE("/by-key/:key", handler.RemoveSecretByKey)

	templateRoutes := v1.Group("/templates")
	templateRoutes.POST("", handler.CreateTemplate)
	templateRoutes.GET("", handler.ListTemplates)
	templateRoutes.POST("/import", handler.ImportTemplate)
	templateRoutes.GET("/:id/export", handler.ExportTemplateByID)
	templateRoutes.GET("/:id", handler.GetTemplateByID)
	templateRoutes.GET("/by-key/:key/export", handler.ExportTemplateByKey)
	templateRoutes.GET("/by-key/:key", handler.GetTemplateByKey)
	templateRoutes.DELETE("/:id", handler.RemoveTemplateByID)
	templateRoutes.DELETE("/by-key/:key", handler.RemoveTemplateByKey)

	clusterRoutes := v1.Group("/cluster")
	nodeRoutes := clusterRoutes.Group("/nodes")
	nodeRoutes.POST("", handler.CreateNode)
	nodeRoutes.GET("", handler.ListNodes)
	nodeRoutes.GET("/:id", handler.GetNodeByID)
	nodeRoutes.GET("/by-key/:key", handler.GetNodeByKey)
	nodeRoutes.DELETE("/:id", handler.RemoveNodeByID)
	nodeRoutes.DELETE("/by-key/:key", handler.RemoveNodeByKey)

	namespaceRoutes := clusterRoutes.Group("/namespaces")
	namespaceRoutes.POST("", handler.CreateNamespace)
	namespaceRoutes.GET("", handler.ListNamespaces)
	namespaceRoutes.GET("/:id", handler.GetNamespaceByID)
	namespaceRoutes.GET("/by-key/:key", handler.GetNamespaceByKey)
	namespaceRoutes.DELETE("/:id", handler.RemoveNamespaceByID)
	namespaceRoutes.DELETE("/by-key/:key", handler.RemoveNamespaceByKey)

	return router
}

// Run starts the Bastion cluster API server and shuts it down when ctx is cancelled.
func Run(ctx context.Context, addr string, db *clusterdb.Client, logger *slog.Logger, opts ...RouterOption) error {
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
			logger.ErrorContext(ctx, "cluster API failed", slog.String("error", err.Error()))
		}

		return err
	case <-ctx.Done():
		logger.InfoContext(context.Background(), "cluster API shutting down", slog.String("addr", addr))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown cluster API: %w", err)
		}

		if err := <-errCh; err != nil {
			return err
		}

		logger.InfoContext(context.Background(), "cluster API stopped", slog.String("addr", addr))

		return nil
	}
}
