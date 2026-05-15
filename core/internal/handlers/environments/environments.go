// Package environments handles environment HTTP routes.
package environments

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

// Handler handles environment route requests.
type Handler struct {
	environments *environment.Service
}

// NewHandler returns an environment route handler.
func NewHandler(service *environment.Service) Handler {
	return Handler{environments: service}
}

// Create handles environment creation requests.
func (h Handler) Create(c *gin.Context) {
	var req environment.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.environments.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusOK)
}

// List handles environment list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	environments, err := h.environments.List(c.Request.Context(), limit, cursor)
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
