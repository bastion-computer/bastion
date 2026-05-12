// Package sandboxes handles sandbox HTTP routes.
package sandboxes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
)

// Handler handles sandbox route requests.
type Handler struct {
	sandboxes *sandbox.Service
}

// NewHandler returns a sandbox route handler.
func NewHandler(service *sandbox.Service) Handler {
	return Handler{sandboxes: service}
}

// Create handles sandbox creation requests.
func (h Handler) Create(c *gin.Context) {
	var req sandbox.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.sandboxes.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusOK)
}

// List handles sandbox list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	sandboxes, err := h.sandboxes.List(c.Request.Context(), limit, cursor)
	handlers.Respond(c, sandboxes, err, http.StatusOK)
}

// Get handles sandbox lookup requests.
func (h Handler) Get(c *gin.Context) {
	sandbox, err := h.sandboxes.Get(c.Request.Context(), c.Param("id"))
	handlers.Respond(c, sandbox, err, http.StatusOK)
}

// Pause handles sandbox pause requests.
func (h Handler) Pause(c *gin.Context) {
	sandbox, err := h.sandboxes.Pause(c.Request.Context(), c.Param("id"))
	handlers.Respond(c, sandbox, err, http.StatusOK)
}

// Remove handles sandbox removal requests.
func (h Handler) Remove(c *gin.Context) {
	sandbox, err := h.sandboxes.Remove(c.Request.Context(), c.Param("id"))
	handlers.Respond(c, sandbox, err, http.StatusOK)
}

// Exec handles sandbox execution requests.
func (h Handler) Exec(c *gin.Context) {
	var req sandbox.ExecRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	response, err := h.sandboxes.Exec(c.Request.Context(), c.Param("id"), req.Command)
	handlers.Respond(c, response, err, http.StatusOK)
}
