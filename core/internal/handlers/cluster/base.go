package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/base"
)

// BuildBase handles cluster base build requests.
func (h Handler) BuildBase(c *gin.Context) {
	force, ok := clusterBoolQuery(c, "force")
	if !ok {
		return
	}

	streamCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newBaseStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	built, err := h.cluster.BuildBase(streamCtx, base.BuildRequest{Force: force, Logs: stream})
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

// GetBase handles cluster base metadata lookup requests.
func (h Handler) GetBase(c *gin.Context) {
	base, err := h.cluster.GetBase(c.Request.Context())
	handlers.Respond(c, base, err, http.StatusOK)
}

// ImportBase handles cluster base archive import requests.
func (h Handler) ImportBase(c *gin.Context) {
	force, ok := clusterBoolQuery(c, "force")
	if !ok {
		return
	}

	streamCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newBaseStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	imported, err := h.cluster.ImportBase(streamCtx, base.ImportRequest{Force: force, Archive: c.Request.Body, ArchiveSize: c.Request.ContentLength, Logs: stream})
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

// ExportBase handles cluster base archive export requests.
func (h Handler) ExportBase(c *gin.Context) {
	c.Header("Content-Type", base.ArchiveContentType)

	if err := h.cluster.ExportBase(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			handlers.Respond(c, nil, err, http.StatusOK)
		}
	}
}

func clusterBoolQuery(c *gin.Context, name string) (bool, bool) {
	value, ok := c.GetQuery(name)
	if !ok || value == "" {
		return false, true
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{clusterErrorKey: "invalid " + name + " query parameter"})

		return false, false
	}

	return parsed, true
}

type baseStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newBaseStream(w http.ResponseWriter, cancel context.CancelFunc) *baseStream {
	return &baseStream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *baseStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *baseStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(base.StreamEvent{Type: base.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *baseStream) Result(baseImage base.Base) error {
	return s.write(base.StreamEvent{Type: base.StreamEventResult, Base: &baseImage})
}

func (s *baseStream) Error(err error) error {
	return s.write(base.StreamEvent{Type: base.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *baseStream) write(event base.StreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *baseStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}
