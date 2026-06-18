// Package utilization handles utilization HTTP routes.
package utilization

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	utilizationservice "github.com/bastion-computer/bastion/core/internal/services/utilization"
)

// Handler handles utilization route requests.
type Handler struct {
	utilization *utilizationservice.Service
}

// NewHandler returns a utilization route handler.
func NewHandler(service *utilizationservice.Service) Handler {
	return Handler{utilization: service}
}

// Get handles utilization requests.
func (h Handler) Get(c *gin.Context) {
	utilization, err := h.utilization.Get(c.Request.Context())
	handlers.Respond(c, utilization, err, http.StatusOK)
}
