package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "request_id"
	unmatchedRoute  = "unmatched"
)

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set(requestIDKey, requestID)
		c.Header(requestIDHeader, requestID)
		c.Next()
	}
}

func slogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		status := c.Writer.Status()
		attrs := []slog.Attr{
			slog.String("request_id", requestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("route", route(c)),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
			slog.Int("body_size", c.Writer.Size()),
		}

		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}

		logger.LogAttrs(c.Request.Context(), levelForStatus(status, len(c.Errors) > 0), "request", attrs...)
	}
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			err := errors.New("panic: " + fmt.Sprint(recovered))
			_ = c.Error(err)
			logger.ErrorContext(c.Request.Context(), "request panic",
				slog.String("request_id", requestID(c)),
				slog.String("method", c.Request.Method),
				slog.String("route", route(c)),
				slog.String("error", err.Error()),
				slog.String("stack", string(debug.Stack())),
			)

			c.AbortWithStatus(http.StatusInternalServerError)
		}()

		c.Next()
	}
}

func requestID(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

func route(c *gin.Context) string {
	path := c.FullPath()
	if path == "" {
		return unmatchedRoute
	}

	return path
}

func levelForStatus(status int, hasError bool) slog.Level {
	switch {
	case status >= http.StatusInternalServerError || hasError && status < http.StatusBadRequest:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
