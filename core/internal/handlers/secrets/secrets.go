// Package secrets handles secret HTTP routes.
package secrets

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

// Handler handles secret route requests.
type Handler struct {
	secrets *secret.Service
}

// NewHandler returns a secret route handler.
func NewHandler(service *secret.Service) Handler {
	return Handler{secrets: service}
}

// Create handles secret creation requests.
func (h Handler) Create(c *gin.Context) {
	var req secret.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.secrets.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusCreated)
}

// List handles secret list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	secrets, err := h.secrets.List(c.Request.Context(), limit, cursor)
	handlers.Respond(c, secrets, err, http.StatusOK)
}

// GetByID handles secret lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, secret, err, http.StatusOK)
}

// GetByKey handles secret lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, secret, err, http.StatusOK)
}

// RemoveByID handles secret removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, secret, err, http.StatusOK)
}

// RemoveByKey handles secret removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, secret, err, http.StatusOK)
}
