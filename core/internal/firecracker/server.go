package firecracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// ServerOptions configures the bastiond server.
type ServerOptions struct {
	SocketPath string
	SocketMode os.FileMode
	SocketUID  int
	SocketGID  int
	Manager    Manager
	Logger     *slog.Logger
}

// RunServer starts bastiond on a Unix socket and shuts it down when ctx is cancelled.
func RunServer(ctx context.Context, opts ServerOptions) error {
	opts = opts.withDefaults()

	listener, err := listenUnixSocket(ctx, opts)
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:           NewRouter(opts.Manager, opts.Logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      240 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return serveUntilDone(ctx, server, listener, opts.SocketPath)
}

func (o ServerOptions) withDefaults() ServerOptions {
	if o.SocketPath == "" {
		o.SocketPath = DefaultSocketPath
	}

	if o.SocketMode == 0 {
		o.SocketMode = 0o660
	}

	if o.Logger == nil {
		o.Logger = slog.Default()
	}

	return o
}

func listenUnixSocket(ctx context.Context, opts ServerOptions) (net.Listener, error) {
	socketDir := filepath.Dir(opts.SocketPath)
	if err := os.MkdirAll(socketDir, 0o750); err != nil {
		return nil, fmt.Errorf("create bastiond socket directory: %w", err)
	}

	if opts.SocketUID != 0 || opts.SocketGID != 0 {
		if err := os.Chown(socketDir, opts.SocketUID, opts.SocketGID); err != nil {
			return nil, fmt.Errorf("chown bastiond socket directory: %w", err)
		}
	}

	if err := os.Remove(opts.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale bastiond socket: %w", err)
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", opts.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on bastiond socket: %w", err)
	}

	if err := os.Chmod(opts.SocketPath, opts.SocketMode); err != nil {
		_ = listener.Close()

		return nil, fmt.Errorf("chmod bastiond socket: %w", err)
	}

	if opts.SocketUID == 0 && opts.SocketGID == 0 {
		return listener, nil
	}

	if err := os.Chown(opts.SocketPath, opts.SocketUID, opts.SocketGID); err != nil {
		_ = listener.Close()

		return nil, fmt.Errorf("chown bastiond socket: %w", err)
	}

	return listener, nil
}

func serveUntilDone(ctx context.Context, server *http.Server, listener net.Listener, socketPath string) error {
	errCh := make(chan error, 1)

	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}

		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return shutdownServer(server, errCh, socketPath)
	}
}

func shutdownServer(server *http.Server, errCh <-chan error, socketPath string) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown bastiond: %w", err)
	}

	if err := <-errCh; err != nil {
		return err
	}

	return os.Remove(socketPath)
}

// NewRouter builds the bastiond Gin router.
func NewRouter(manager Manager, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(daemonLoggingMiddleware(logger), gin.Recovery())

	v1 := router.Group("/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1.POST("/vms", func(c *gin.Context) {
		var req LaunchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})

			return
		}

		vm, err := manager.Launch(c.Request.Context(), req)
		respondDaemon(c, vm, err)
	})

	v1.GET("/vms/:id", func(c *gin.Context) {
		vm, err := manager.State(c.Request.Context(), c.Param("id"))
		respondDaemon(c, vm, err)
	})

	v1.DELETE("/vms/:id", func(c *gin.Context) {
		vm, err := manager.Remove(c.Request.Context(), c.Param("id"))
		respondDaemon(c, vm, err)
	})

	return router
}

func respondDaemon(c *gin.Context, value any, err error) {
	if err != nil {
		_ = c.Error(err)

		status := http.StatusInternalServerError
		if errors.Is(err, ErrVMInitFailed) {
			status = http.StatusFailedDependency
		}

		c.JSON(status, gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, value)
}

func daemonLoggingMiddleware(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("route", c.FullPath()),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
			slog.Int("body_size", c.Writer.Size()),
		}

		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}

		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "bastiond request", attrs...)
	}
}
