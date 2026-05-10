// Package api exposes the local Bastion HTTP API.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/api/checkpoints"
	"github.com/bastion-computer/bastion/core/internal/api/sandboxes"
	"github.com/bastion-computer/bastion/core/internal/api/secrets"
	"github.com/bastion-computer/bastion/core/internal/api/templates"
	"github.com/bastion-computer/bastion/core/internal/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/sandbox"
	"github.com/bastion-computer/bastion/core/internal/secret"
	"github.com/bastion-computer/bastion/core/internal/template"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// NewServer returns an HTTP server configured for the Bastion API.
func NewServer(addr string, db *database.Client) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// NewRouter builds the Bastion API router.
func NewRouter(db *database.Client) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	secretHandler := secrets.NewHandler(secret.New(db))
	secretRoutes := v1.Group("/secrets")
	secretRoutes.POST("", secretHandler.Create)
	secretRoutes.GET("", secretHandler.List)
	secretRoutes.GET("/:id", secretHandler.GetByID)
	secretRoutes.GET("/by-key/:key", secretHandler.GetByKey)
	secretRoutes.DELETE("/:id", secretHandler.RemoveByID)
	secretRoutes.DELETE("/by-key/:key", secretHandler.RemoveByKey)
	secretRoutes.POST("/resolve", secretHandler.Resolve)

	templateHandler := templates.NewHandler(template.New(db))
	templateRoutes := v1.Group("/templates")
	templateRoutes.POST("", templateHandler.Create)
	templateRoutes.GET("", templateHandler.List)
	templateRoutes.GET("/:id", templateHandler.GetByID)
	templateRoutes.GET("/by-key/:key", templateHandler.GetByKey)
	templateRoutes.DELETE("/:id", templateHandler.RemoveByID)
	templateRoutes.DELETE("/by-key/:key", templateHandler.RemoveByKey)

	sandboxHandler := sandboxes.NewHandler(sandbox.New(db))
	sandboxRoutes := v1.Group("/sandboxes")
	sandboxRoutes.POST("", sandboxHandler.Create)
	sandboxRoutes.GET("", sandboxHandler.List)
	sandboxRoutes.GET("/:id", sandboxHandler.Get)
	sandboxRoutes.POST("/:id/pause", sandboxHandler.Pause)
	sandboxRoutes.POST("/:id/exec", sandboxHandler.Exec)
	sandboxRoutes.DELETE("/:id", sandboxHandler.Remove)

	checkpointHandler := checkpoints.NewHandler(checkpoint.New(db))
	checkpointRoutes := v1.Group("/checkpoints")
	checkpointRoutes.POST("", checkpointHandler.Create)
	checkpointRoutes.GET("", checkpointHandler.List)
	checkpointRoutes.GET("/:id", checkpointHandler.GetByID)
	checkpointRoutes.GET("/by-key/:key", checkpointHandler.GetByKey)
	checkpointRoutes.DELETE("/:id", checkpointHandler.RemoveByID)
	checkpointRoutes.DELETE("/by-key/:key", checkpointHandler.RemoveByKey)

	return router
}

// Run starts the Bastion API server and shuts it down when ctx is cancelled.
func Run(ctx context.Context, addr string, db *database.Client) error {
	server := NewServer(addr, db)
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
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}

		return <-errCh
	}
}
