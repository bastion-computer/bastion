// Package base handles base image HTTP routes.
package base

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	baseservice "github.com/bastion-computer/bastion/core/internal/services/base"
)

// Handler handles base route requests.
type Handler struct {
	base *baseservice.Service
}

// NewHandler returns a base route handler.
func NewHandler(service *baseservice.Service) Handler {
	return Handler{base: service}
}

// Get handles base metadata lookup requests.
func (h Handler) Get(c *gin.Context) {
	base, err := h.base.Get(c.Request.Context())
	handlers.Respond(c, base, err, http.StatusOK)
}

// Build handles base build requests.
func (h Handler) Build(c *gin.Context) {
	force, ok := boolQuery(c, "force")
	if !ok {
		return
	}

	streamCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	built, err := h.base.Build(streamCtx, baseservice.BuildRequest{Force: force, Logs: stream})
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(built); err != nil {
		_ = c.Error(err)
	}
}

// Import handles base archive import requests.
func (h Handler) Import(c *gin.Context) {
	force, ok := boolQuery(c, "force")
	if !ok {
		return
	}

	streamCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	imported, err := h.base.Import(streamCtx, baseservice.ImportRequest{Force: force, Archive: c.Request.Body, ArchiveSize: c.Request.ContentLength, Logs: stream})
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(imported); err != nil {
		_ = c.Error(err)
	}
}

// Export handles base archive export requests.
func (h Handler) Export(c *gin.Context) {
	c.Header("Content-Type", baseservice.ArchiveContentType)

	if err := h.base.Export(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			handlers.Respond(c, nil, err, http.StatusOK)
		}
	}
}

func boolQuery(c *gin.Context, name string) (bool, bool) {
	value, ok := c.GetQuery(name)
	if !ok || value == "" {
		return false, true
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name + " query parameter"})

		return false, false
	}

	return parsed, true
}

type stream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newStream(w http.ResponseWriter, cancel context.CancelFunc) *stream {
	return &stream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *stream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *stream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(baseservice.StreamEvent{Type: baseservice.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *stream) Result(base baseservice.Base) error {
	return s.write(baseservice.StreamEvent{Type: baseservice.StreamEventResult, Base: &base})
}

func (s *stream) Error(err error) error {
	return s.write(baseservice.StreamEvent{Type: baseservice.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *stream) write(event baseservice.StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *stream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}
