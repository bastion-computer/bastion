// Package environments handles environment HTTP routes.
package environments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/sshtunnel"
)

// SSHRunner runs an upgraded API SSH stream.
type SSHRunner func(context.Context, io.ReadWriteCloser, environment.SSHConnection, sshtunnel.Request) error

// Option configures environment route handlers.
type Option func(*Handler)

// Handler handles environment route requests.
type Handler struct {
	environments *environment.Service
	sshRunner    SSHRunner
}

// NewHandler returns an environment route handler.
func NewHandler(service *environment.Service, opts ...Option) Handler {
	h := Handler{environments: service, sshRunner: runSSHSession}
	for _, opt := range opts {
		opt(&h)
	}

	if h.sshRunner == nil {
		h.sshRunner = runSSHSession
	}

	return h
}

// WithSSHRunner overrides the SSH stream runner.
func WithSSHRunner(runner SSHRunner) Option {
	return func(h *Handler) {
		h.sshRunner = runner
	}
}

// Create handles environment creation requests.
func (h Handler) Create(c *gin.Context) {
	var req environment.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	createCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newCreateStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	req.Logs = stream

	created, err := h.environments.Create(createCtx, req)
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(created); err != nil {
		_ = c.Error(err)
	}
}

// List handles environment list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	environments, err := h.environments.List(c.Request.Context(), limit, cursor, c.QueryArray("tag"))
	handlers.Respond(c, environments, err, http.StatusOK)
}

// Get handles environment lookup requests.
func (h Handler) Get(c *gin.Context) {
	environment, err := h.environments.Get(c.Request.Context(), c.Param("id"))
	handlers.Respond(c, environment, err, http.StatusOK)
}

// Remove handles environment removal requests.
func (h Handler) Remove(c *gin.Context) {
	environment, err := h.environments.Remove(c.Request.Context(), c.Param("id"))
	handlers.Respond(c, environment, err, http.StatusOK)
}

type createStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newCreateStream(w http.ResponseWriter, cancel context.CancelFunc) *createStream {
	return &createStream{
		w:          w,
		encoder:    json.NewEncoder(w),
		controller: http.NewResponseController(w),
		cancel:     cancel,
	}
}

func (s *createStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *createStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(environment.CreateStreamEvent{Type: environment.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *createStream) Result(created environment.Environment) error {
	return s.write(environment.CreateStreamEvent{Type: environment.StreamEventResult, Environment: &created})
}

func (s *createStream) Error(err error) error {
	return s.write(environment.CreateStreamEvent{Type: environment.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *createStream) write(event environment.CreateStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *createStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}
