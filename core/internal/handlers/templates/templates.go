// Package templates handles template HTTP routes.
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

// Handler handles template route requests.
type Handler struct {
	templates *template.Service
}

// NewHandler returns a template route handler.
func NewHandler(service *template.Service) Handler {
	return Handler{templates: service}
}

// Create handles template creation requests.
func (h Handler) Create(c *gin.Context) {
	var req template.CreateRequest
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

	created, err := h.templates.Create(createCtx, req)
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

// List handles template list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	templates, err := h.templates.List(c.Request.Context(), limit, cursor)
	handlers.Respond(c, templates, err, http.StatusOK)
}

// GetByID handles template lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	template, err := h.templates.Get(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, template, err, http.StatusOK)
}

// GetByKey handles template lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	template, err := h.templates.Get(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, template, err, http.StatusOK)
}

// ExportByID handles template export by ID requests.
func (h Handler) ExportByID(c *gin.Context) {
	h.export(c, c.Param("id"), "")
}

// ExportByKey handles template export by key requests.
func (h Handler) ExportByKey(c *gin.Context) {
	h.export(c, "", c.Param("key"))
}

// Import handles template import requests.
func (h Handler) Import(c *gin.Context) {
	var key *string
	if value, ok := c.GetQuery("key"); ok {
		key = &value
	}

	imported, err := h.templates.Import(c.Request.Context(), template.ImportRequest{Key: key, Archive: c.Request.Body, ArchiveSize: c.Request.ContentLength})
	handlers.Respond(c, imported, err, http.StatusCreated)
}

func (h Handler) export(c *gin.Context, id, key string) {
	c.Header("Content-Type", template.ArchiveContentType)

	if err := h.templates.Export(c.Request.Context(), id, key, c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			handlers.Respond(c, nil, err, http.StatusOK)
		}
	}
}

// RemoveByID handles template removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	template, err := h.templates.Remove(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, template, err, http.StatusOK)
}

// RemoveByKey handles template removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	template, err := h.templates.Remove(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, template, err, http.StatusOK)
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

	if err := s.write(template.CreateStreamEvent{Type: template.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *createStream) Result(created template.Metadata) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &created})
}

func (s *createStream) Error(err error) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *createStream) write(event template.CreateStreamEvent) error {
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
