// Package secrets handles secret HTTP routes.
package secrets

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/api/httputil"
	"github.com/bastion-computer/bastion/core/internal/secret"
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
	if !httputil.BindJSON(c, &req) {
		return
	}

	created, err := h.secrets.Create(c.Request.Context(), req)
	httputil.Respond(c, created, err, http.StatusOK)
}

// List handles secret list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := httputil.ListParams(c)
	secrets, err := h.secrets.List(c.Request.Context(), limit, cursor)
	httputil.Respond(c, secrets, err, http.StatusOK)
}

// GetByID handles secret lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), c.Param("id"), "")
	httputil.Respond(c, secret, err, http.StatusOK)
}

// GetByKey handles secret lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), "", c.Param("key"))
	httputil.Respond(c, secret, err, http.StatusOK)
}

// Resolve handles secret resolve requests.
func (h Handler) Resolve(c *gin.Context) {
	var req secret.ResolveRequest
	if !httputil.BindJSON(c, &req) {
		return
	}

	value, err := h.secrets.Resolve(c.Request.Context(), req.ID, req.Key)
	httputil.Respond(c, value, err, http.StatusOK)
}

// RemoveByID handles secret removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), c.Param("id"), "")
	httputil.Respond(c, secret, err, http.StatusOK)
}

// RemoveByKey handles secret removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), "", c.Param("key"))
	httputil.Respond(c, secret, err, http.StatusOK)
}
