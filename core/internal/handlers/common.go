// Package handlers contains shared HTTP handler helpers.
package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

// BindJSON parses a JSON request body and writes a 400 response on failure.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}

	return true
}

// Respond writes a successful JSON response or maps a domain error to HTTP.
func Respond(c *gin.Context, value any, err error, okStatus int) {
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(okStatus, value)
}

// ListParams returns normalized pagination query parameters.
func ListParams(c *gin.Context) (int, string) {
	limit := 20

	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	return limit, c.Query("cursor")
}

func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError

	switch {
	case errors.Is(err, failure.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, failure.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, failure.ErrConflict):
		status = http.StatusConflict
	}

	c.JSON(status, gin.H{"error": err.Error()})
}
