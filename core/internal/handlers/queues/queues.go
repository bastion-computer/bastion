// Package queues handles queue HTTP routes.
package queues

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

// Handler handles queue route requests.
type Handler struct {
	queues *queue.Service
}

// NewHandler returns a queue route handler.
func NewHandler(service *queue.Service) Handler {
	return Handler{queues: service}
}

// Create handles queue creation requests.
func (h Handler) Create(c *gin.Context) {
	var req queue.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.queues.Create(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusOK)
}

// List handles queue list requests.
func (h Handler) List(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	queues, err := h.queues.List(c.Request.Context(), limit, cursor)
	handlers.Respond(c, queues, err, http.StatusOK)
}

// GetByID handles queue lookup by ID requests.
func (h Handler) GetByID(c *gin.Context) {
	queue, err := h.queues.Get(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, queue, err, http.StatusOK)
}

// GetByKey handles queue lookup by key requests.
func (h Handler) GetByKey(c *gin.Context) {
	queue, err := h.queues.Get(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, queue, err, http.StatusOK)
}

// RemoveByID handles queue removal by ID requests.
func (h Handler) RemoveByID(c *gin.Context) {
	queue, err := h.queues.Remove(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, queue, err, http.StatusOK)
}

// RemoveByKey handles queue removal by key requests.
func (h Handler) RemoveByKey(c *gin.Context) {
	queue, err := h.queues.Remove(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, queue, err, http.StatusOK)
}

// PublishByID handles queue task publication by queue ID.
func (h Handler) PublishByID(c *gin.Context) {
	var req queue.PublishRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Publish(c.Request.Context(), c.Param("id"), "", req)
	handlers.Respond(c, task, err, http.StatusOK)
}

// PublishByKey handles queue task publication by queue key.
func (h Handler) PublishByKey(c *gin.Context) {
	var req queue.PublishRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Publish(c.Request.Context(), "", c.Param("key"), req)
	handlers.Respond(c, task, err, http.StatusOK)
}

// GetTaskByID handles queue task lookup by queue ID.
func (h Handler) GetTaskByID(c *gin.Context) {
	task, err := h.queues.GetTask(c.Request.Context(), c.Param("id"), "", c.Param("taskID"))
	handlers.Respond(c, task, err, http.StatusOK)
}

// GetTaskByKey handles queue task lookup by queue key.
func (h Handler) GetTaskByKey(c *gin.Context) {
	task, err := h.queues.GetTask(c.Request.Context(), "", c.Param("key"), c.Param("taskID"))
	handlers.Respond(c, task, err, http.StatusOK)
}

// LeaseByID handles task leasing by queue ID.
func (h Handler) LeaseByID(c *gin.Context) {
	var req queue.LeaseRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, ok, err := h.queues.Lease(c.Request.Context(), c.Param("id"), "", req)
	if err != nil {
		handlers.Respond(c, nil, err, http.StatusOK)

		return
	}

	if !ok {
		c.Status(http.StatusNoContent)

		return
	}

	c.JSON(http.StatusOK, task)
}

// LeaseByKey handles task leasing by queue key.
func (h Handler) LeaseByKey(c *gin.Context) {
	var req queue.LeaseRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, ok, err := h.queues.Lease(c.Request.Context(), "", c.Param("key"), req)
	if err != nil {
		handlers.Respond(c, nil, err, http.StatusOK)

		return
	}

	if !ok {
		c.Status(http.StatusNoContent)

		return
	}

	c.JSON(http.StatusOK, task)
}

// AckByID handles task ACK by queue ID.
func (h Handler) AckByID(c *gin.Context) {
	var req queue.AckRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Ack(c.Request.Context(), c.Param("id"), "", c.Param("taskID"), req)
	handlers.Respond(c, task, err, http.StatusOK)
}

// AckByKey handles task ACK by queue key.
func (h Handler) AckByKey(c *gin.Context) {
	var req queue.AckRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Ack(c.Request.Context(), "", c.Param("key"), c.Param("taskID"), req)
	handlers.Respond(c, task, err, http.StatusOK)
}

// FailByID handles task failure by queue ID.
func (h Handler) FailByID(c *gin.Context) {
	var req queue.FailRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Fail(c.Request.Context(), c.Param("id"), "", c.Param("taskID"), req)
	handlers.Respond(c, task, err, http.StatusOK)
}

// FailByKey handles task failure by queue key.
func (h Handler) FailByKey(c *gin.Context) {
	var req queue.FailRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	task, err := h.queues.Fail(c.Request.Context(), "", c.Param("key"), c.Param("taskID"), req)
	handlers.Respond(c, task, err, http.StatusOK)
}
