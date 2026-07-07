// Package cluster handles cluster control plane HTTP routes.
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
)

const clusterErrorKey = "error"

// Handler handles cluster route requests.
type Handler struct {
	cluster *clusterservice.Service
}

// NewHandler returns a cluster route handler.
func NewHandler(service *clusterservice.Service) Handler {
	return Handler{cluster: service}
}

// CreateNode handles cluster node creation requests.
func (h Handler) CreateNode(c *gin.Context) {
	var req clusterservice.CreateNodeRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	createCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newNodeCreateStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	req.Logs = stream

	node, err := h.cluster.CreateNode(createCtx, req)
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(node); err != nil {
		_ = c.Error(err)
	}
}

// ListNodes handles cluster node list requests.
func (h Handler) ListNodes(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	nodes, err := h.cluster.ListNodes(c.Request.Context(), limit, cursor)
	handlers.Respond(c, nodes, err, http.StatusOK)
}

// GetNodeByID handles cluster node lookup by ID requests.
func (h Handler) GetNodeByID(c *gin.Context) {
	node, err := h.cluster.GetNode(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, node, err, http.StatusOK)
}

// GetNodeByKey handles cluster node lookup by key requests.
func (h Handler) GetNodeByKey(c *gin.Context) {
	node, err := h.cluster.GetNode(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, node, err, http.StatusOK)
}

// RemoveNodeByID handles cluster node removal by ID requests.
func (h Handler) RemoveNodeByID(c *gin.Context) {
	node, err := h.cluster.RemoveNode(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, node, err, http.StatusOK)
}

// RemoveNodeByKey handles cluster node removal by key requests.
func (h Handler) RemoveNodeByKey(c *gin.Context) {
	node, err := h.cluster.RemoveNode(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, node, err, http.StatusOK)
}

// CreateNamespace handles cluster namespace creation requests.
func (h Handler) CreateNamespace(c *gin.Context) {
	var req clusterservice.CreateNamespaceRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	namespace, err := h.cluster.CreateNamespace(c.Request.Context(), req)
	handlers.Respond(c, namespace, err, http.StatusCreated)
}

// ListNamespaces handles cluster namespace list requests.
func (h Handler) ListNamespaces(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	namespaces, err := h.cluster.ListNamespaces(c.Request.Context(), limit, cursor)
	handlers.Respond(c, namespaces, err, http.StatusOK)
}

// GetNamespaceByID handles cluster namespace lookup by ID requests.
func (h Handler) GetNamespaceByID(c *gin.Context) {
	namespace, err := h.cluster.GetNamespace(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, namespace, err, http.StatusOK)
}

// GetNamespaceByKey handles cluster namespace lookup by key requests.
func (h Handler) GetNamespaceByKey(c *gin.Context) {
	namespace, err := h.cluster.GetNamespace(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, namespace, err, http.StatusOK)
}

// RemoveNamespaceByID handles cluster namespace removal by ID requests.
func (h Handler) RemoveNamespaceByID(c *gin.Context) {
	namespace, err := h.cluster.RemoveNamespace(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, namespace, err, http.StatusOK)
}

// RemoveNamespaceByKey handles cluster namespace removal by key requests.
func (h Handler) RemoveNamespaceByKey(c *gin.Context) {
	namespace, err := h.cluster.RemoveNamespace(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, namespace, err, http.StatusOK)
}

// Health handles aggregate cluster health requests.
func (h Handler) Health(c *gin.Context) {
	health, err := h.cluster.Health(c.Request.Context())
	handlers.Respond(c, health, err, http.StatusOK)
}

// Utilization handles aggregate cluster utilization requests.
func (h Handler) Utilization(c *gin.Context) {
	utilization, err := h.cluster.Utilization(c.Request.Context())
	handlers.Respond(c, utilization, err, http.StatusOK)
}

type nodeCreateStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newNodeCreateStream(w http.ResponseWriter, cancel context.CancelFunc) *nodeCreateStream {
	return &nodeCreateStream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *nodeCreateStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *nodeCreateStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(clusterservice.NodeStreamEvent{Type: clusterservice.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *nodeCreateStream) Result(node clusterservice.Node) error {
	return s.write(clusterservice.NodeStreamEvent{Type: clusterservice.StreamEventResult, Node: &node})
}

func (s *nodeCreateStream) Error(err error) error {
	return s.write(clusterservice.NodeStreamEvent{Type: clusterservice.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *nodeCreateStream) write(event clusterservice.NodeStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *nodeCreateStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}
