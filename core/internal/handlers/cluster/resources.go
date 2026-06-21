package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

// CreateSecret handles source secret creation requests.
func (h Handler) CreateSecret(c *gin.Context) {
	var req secret.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.cluster.CreateSecret(c.Request.Context(), namespaceSelector(c), req)
	handlers.Respond(c, created, err, http.StatusCreated)
}

// ListSecrets handles source secret list requests.
func (h Handler) ListSecrets(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	secrets, err := h.cluster.ListSecrets(c.Request.Context(), namespaceSelector(c), limit, cursor)
	handlers.Respond(c, secrets, err, http.StatusOK)
}

// GetSecretByID handles source secret lookup by ID requests.
func (h Handler) GetSecretByID(c *gin.Context) {
	secret, err := h.cluster.GetSecret(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, secret, err, http.StatusOK)
}

// GetSecretByKey handles source secret lookup by key requests.
func (h Handler) GetSecretByKey(c *gin.Context) {
	secret, err := h.cluster.GetSecret(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, secret, err, http.StatusOK)
}

// RemoveSecretByID handles source secret removal by ID requests.
func (h Handler) RemoveSecretByID(c *gin.Context) {
	secret, err := h.cluster.RemoveSecret(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, secret, err, http.StatusOK)
}

// RemoveSecretByKey handles source secret removal by key requests.
func (h Handler) RemoveSecretByKey(c *gin.Context) {
	secret, err := h.cluster.RemoveSecret(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, secret, err, http.StatusOK)
}

// CreateTemplate handles source template creation requests.
func (h Handler) CreateTemplate(c *gin.Context) {
	var req template.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	createCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newTemplateCreateStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	req.Logs = stream

	created, err := h.cluster.CreateTemplate(createCtx, namespaceSelector(c), req)
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

// ImportTemplate handles source template import requests.
func (h Handler) ImportTemplate(c *gin.Context) {
	var key *string
	if value, ok := c.GetQuery("key"); ok {
		key = &value
	}

	imported, err := h.cluster.ImportTemplate(c.Request.Context(), namespaceSelector(c), template.ImportRequest{Key: key, Archive: c.Request.Body, ArchiveSize: c.Request.ContentLength})
	handlers.Respond(c, imported, err, http.StatusCreated)
}

// ListTemplates handles source template list requests.
func (h Handler) ListTemplates(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	templates, err := h.cluster.ListTemplates(c.Request.Context(), namespaceSelector(c), limit, cursor)
	handlers.Respond(c, templates, err, http.StatusOK)
}

// GetTemplateByID handles source template lookup by ID requests.
func (h Handler) GetTemplateByID(c *gin.Context) {
	template, err := h.cluster.GetTemplate(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, template, err, http.StatusOK)
}

// GetTemplateByKey handles source template lookup by key requests.
func (h Handler) GetTemplateByKey(c *gin.Context) {
	template, err := h.cluster.GetTemplate(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, template, err, http.StatusOK)
}

// ExportTemplateByID handles source template export by ID requests.
func (h Handler) ExportTemplateByID(c *gin.Context) {
	h.exportTemplate(c, c.Param("id"), "")
}

// ExportTemplateByKey handles source template export by key requests.
func (h Handler) ExportTemplateByKey(c *gin.Context) {
	h.exportTemplate(c, "", c.Param("key"))
}

func (h Handler) exportTemplate(c *gin.Context, id, key string) {
	c.Header("Content-Type", template.ArchiveContentType)

	if err := h.cluster.ExportTemplate(c.Request.Context(), namespaceSelector(c), id, key, c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			handlers.Respond(c, nil, err, http.StatusOK)
		}
	}
}

// RemoveTemplateByID handles source template removal by ID requests.
func (h Handler) RemoveTemplateByID(c *gin.Context) {
	template, err := h.cluster.RemoveTemplate(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, template, err, http.StatusOK)
}

// RemoveTemplateByKey handles source template removal by key requests.
func (h Handler) RemoveTemplateByKey(c *gin.Context) {
	template, err := h.cluster.RemoveTemplate(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, template, err, http.StatusOK)
}

func namespaceSelector(c *gin.Context) clusterservice.NamespaceSelector {
	return clusterservice.NamespaceSelector{ID: c.Query("namespace-id"), Key: c.Query("namespace-key")}
}

type templateCreateStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newTemplateCreateStream(w http.ResponseWriter, cancel context.CancelFunc) *templateCreateStream {
	return &templateCreateStream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *templateCreateStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *templateCreateStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(template.CreateStreamEvent{Type: template.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *templateCreateStream) Result(created template.Metadata) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &created})
}

func (s *templateCreateStream) Error(err error) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *templateCreateStream) write(event template.CreateStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *templateCreateStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}
