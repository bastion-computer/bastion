// Package templates handles template HTTP routes.
package templates

import (
	"net/http"

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

	created, err := h.templates.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusOK)
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
