// Package checkpoints handles checkpoint HTTP routes.
package checkpoints

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/checkpoint"
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
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.checkpoints.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusOK)
}

// List handles checkpoint list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	checkpoints, err := h.checkpoints.List(c.Request.Context(), limit, cursor)
	handlers.Respond(c, checkpoints, err, http.StatusOK)
}

// GetByID handles checkpoint lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, checkpoint, err, http.StatusOK)
}

// GetByKey handles checkpoint lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, checkpoint, err, http.StatusOK)
}

// RemoveByID handles checkpoint removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, checkpoint, err, http.StatusOK)
}

// RemoveByKey handles checkpoint removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, checkpoint, err, http.StatusOK)
}
