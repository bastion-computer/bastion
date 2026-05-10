package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/database"
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

	h := newHandler(db)
	v1 := router.Group("/v1")
	v1.GET("/health", h.health)

	secrets := v1.Group("/secrets")
	secrets.POST("", h.createSecret)
	secrets.GET("", h.listSecrets)
	secrets.GET("/:id", h.getSecretByID)
	secrets.GET("/by-key/:key", h.getSecretByKey)
	secrets.DELETE("/:id", h.removeSecretByID)
	secrets.DELETE("/by-key/:key", h.removeSecretByKey)
	secrets.POST("/resolve", h.resolveSecret)

	templates := v1.Group("/templates")
	templates.POST("", h.createTemplate)
	templates.GET("", h.listTemplates)
	templates.GET("/:id", h.getTemplateByID)
	templates.GET("/by-key/:key", h.getTemplateByKey)
	templates.DELETE("/:id", h.removeTemplateByID)
	templates.DELETE("/by-key/:key", h.removeTemplateByKey)

	sandboxes := v1.Group("/sandboxes")
	sandboxes.POST("", h.createSandbox)
	sandboxes.GET("", h.listSandboxes)
	sandboxes.GET("/:id", h.getSandbox)
	sandboxes.POST("/:id/pause", h.pauseSandbox)
	sandboxes.POST("/:id/exec", h.execSandbox)
	sandboxes.DELETE("/:id", h.removeSandbox)

	checkpoints := v1.Group("/checkpoints")
	checkpoints.POST("", h.createCheckpoint)
	checkpoints.GET("", h.listCheckpoints)
	checkpoints.GET("/:id", h.getCheckpointByID)
	checkpoints.GET("/by-key/:key", h.getCheckpointByKey)
	checkpoints.DELETE("/:id", h.removeCheckpointByID)
	checkpoints.DELETE("/by-key/:key", h.removeCheckpointByKey)

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
