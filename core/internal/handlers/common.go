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
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})

		return false
	}

	return true
}

// Respond writes a successful JSON response or maps a domain error to HTTP.
func Respond(c *gin.Context, value any, err error, okStatus int) {
	if err != nil {
		_ = c.Error(err)
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
	c.JSON(ErrorStatus(err), gin.H{"error": err.Error()})
}

// ErrorStatus maps a domain error to an HTTP status code.
func ErrorStatus(err error) int {
	switch {
	case errors.Is(err, failure.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, failure.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, failure.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, failure.ErrFailedDependency):
		return http.StatusFailedDependency
	}

	return http.StatusInternalServerError
}
