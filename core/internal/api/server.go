// Package api exposes the local Bastion HTTP API.
package api

import (
	"context"
	"errors"
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

	templateHandler := templates.NewHandler(template.NewService(db))
	templateRoutes := v1.Group("/templates")
	templateRoutes.POST("", templateHandler.Create)
	templateRoutes.GET("", templateHandler.List)
	templateRoutes.GET("/:id", templateHandler.GetByID)
	templateRoutes.GET("/by-key/:key", templateHandler.GetByKey)
	templateRoutes.DELETE("/:id", templateHandler.RemoveByID)
	templateRoutes.DELETE("/by-key/:key", templateHandler.RemoveByKey)

	environmentHandler := environments.NewHandler(environment.NewService(db))
	environmentRoutes := v1.Group("/environments")
	environmentRoutes.POST("", environmentHandler.Create)
	environmentRoutes.GET("", environmentHandler.List)
	environmentRoutes.GET("/:id", environmentHandler.Get)
	environmentRoutes.DELETE("/:id", environmentHandler.Remove)

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
