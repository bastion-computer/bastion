package cloudhypervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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

	v1.POST("/templates", func(c *gin.Context) {
		serveDaemonCreate(c, newDaemonTemplateStreamAdapter, func(req *PrepareTemplateRequest, logs io.Writer) {
			req.Logs = logs
		}, manager.PrepareTemplate)
	})

	v1.DELETE("/templates/:id", func(c *gin.Context) {
		prepared, err := manager.RemoveTemplate(c.Request.Context(), c.Param("id"))
		respondDaemon(c, prepared, err)
	})

	v1.POST("/vms", func(c *gin.Context) {
		serveDaemonCreate(c, newDaemonLaunchStreamAdapter, func(req *LaunchRequest, logs io.Writer) {
			req.Logs = logs
		}, manager.Launch)
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

type daemonCreateStream[T any] interface {
	io.Writer
	Start() error
	Result(T) error
	Error(T, error) error
}

func serveDaemonCreate[TReq, TResult any](
	c *gin.Context,
	newStream func(http.ResponseWriter, context.CancelFunc) daemonCreateStream[TResult],
	setLogs func(*TReq, io.Writer),
	run func(context.Context, TReq) (TResult, error),
) {
	var req TReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})

		return
	}

	streamCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	setLogs(&req, stream)

	result, err := run(streamCtx, req)
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(result, err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(result); err != nil {
		_ = c.Error(err)
	}
}

func newDaemonTemplateStreamAdapter(w http.ResponseWriter, cancel context.CancelFunc) daemonCreateStream[PreparedTemplate] {
	return newDaemonTemplateStream(w, cancel)
}

func newDaemonLaunchStreamAdapter(w http.ResponseWriter, cancel context.CancelFunc) daemonCreateStream[VM] {
	return newDaemonLaunchStream(w, cancel)
}

func respondDaemon(c *gin.Context, value any, err error) {
	if err != nil {
		_ = c.Error(err)
		c.JSON(daemonStatusForError(err), gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, value)
}

func daemonStatusForError(err error) int {
	if errors.Is(err, ErrVMInitFailed) {
		return http.StatusFailedDependency
	}

	return http.StatusInternalServerError
}

type daemonLaunchStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

type daemonTemplateStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newDaemonTemplateStream(w http.ResponseWriter, cancel context.CancelFunc) *daemonTemplateStream {
	return &daemonTemplateStream{
		w:          w,
		encoder:    json.NewEncoder(w),
		controller: http.NewResponseController(w),
		cancel:     cancel,
	}
}

func (s *daemonTemplateStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *daemonTemplateStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(PrepareTemplateStreamEvent{Type: StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *daemonTemplateStream) Result(prepared PreparedTemplate) error {
	return s.write(PrepareTemplateStreamEvent{Type: StreamEventResult, Template: &prepared})
}

func (s *daemonTemplateStream) Error(prepared PreparedTemplate, err error) error {
	event := PrepareTemplateStreamEvent{Type: StreamEventError, Error: err.Error(), Status: daemonStatusForError(err)}
	if prepared.TemplateID != "" {
		event.Template = &prepared
	}

	return s.write(event)
}

func (s *daemonTemplateStream) write(event PrepareTemplateStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *daemonTemplateStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}

func newDaemonLaunchStream(w http.ResponseWriter, cancel context.CancelFunc) *daemonLaunchStream {
	return &daemonLaunchStream{
		w:          w,
		encoder:    json.NewEncoder(w),
		controller: http.NewResponseController(w),
		cancel:     cancel,
	}
}

func (s *daemonLaunchStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *daemonLaunchStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(LaunchStreamEvent{Type: StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *daemonLaunchStream) Result(vm VM) error {
	return s.write(LaunchStreamEvent{Type: StreamEventResult, VM: &vm})
}

func (s *daemonLaunchStream) Error(vm VM, err error) error {
	event := LaunchStreamEvent{Type: StreamEventError, Error: err.Error(), Status: daemonStatusForError(err)}
	if vm.EnvironmentID != "" {
		event.VM = &vm
	}

	return s.write(event)
}

func (s *daemonLaunchStream) write(event LaunchStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *daemonLaunchStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
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
