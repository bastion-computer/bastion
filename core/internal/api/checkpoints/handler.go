// Package checkpoints handles checkpoint HTTP routes.
package checkpoints

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/api/httputil"
	"github.com/bastion-computer/bastion/core/internal/checkpoint"
)

// Handler handles checkpoint route requests.
type Handler struct {
	checkpoints *checkpoint.Service
}

// NewHandler returns a checkpoint route handler.
func NewHandler(service *checkpoint.Service) Handler {
	return Handler{checkpoints: service}
}

// Create handles checkpoint creation requests.
func (h Handler) Create(c *gin.Context) {
	var req checkpoint.CreateRequest
	if !httputil.BindJSON(c, &req) {
		return
	}

	created, err := h.checkpoints.Create(c.Request.Context(), req)
	httputil.Respond(c, created, err, http.StatusOK)
}

// List handles checkpoint list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := httputil.ListParams(c)
	checkpoints, err := h.checkpoints.List(c.Request.Context(), limit, cursor)
	httputil.Respond(c, checkpoints, err, http.StatusOK)
}

// GetByID handles checkpoint lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), c.Param("id"), "")
	httputil.Respond(c, checkpoint, err, http.StatusOK)
}

// GetByKey handles checkpoint lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), "", c.Param("key"))
	httputil.Respond(c, checkpoint, err, http.StatusOK)
}

// RemoveByID handles checkpoint removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), c.Param("id"), "")
	httputil.Respond(c, checkpoint, err, http.StatusOK)
}

// RemoveByKey handles checkpoint removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), "", c.Param("key"))
	httputil.Respond(c, checkpoint, err, http.StatusOK)
}
