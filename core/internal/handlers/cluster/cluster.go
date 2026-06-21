// Package cluster handles cluster control plane HTTP routes.
package cluster

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
)

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

	node, err := h.cluster.CreateNode(c.Request.Context(), req)
	handlers.Respond(c, node, err, http.StatusCreated)
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
