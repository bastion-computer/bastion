// Package api exposes the local Bastion HTTP API.
package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/sandbox"
	"github.com/bastion-computer/bastion/core/internal/secret"
	templatepkg "github.com/bastion-computer/bastion/core/internal/template"
)

type handler struct {
	checkpoints *checkpoint.Service
	sandboxes   *sandbox.Service
	secrets     *secret.Service
	templates   *templatepkg.Service
}

func newHandler(db *database.Client) handler {
	return handler{
		checkpoints: checkpoint.New(db),
		sandboxes:   sandbox.New(db),
		secrets:     secret.New(db),
		templates:   templatepkg.New(db),
	}
}

func (h handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h handler) createSecret(c *gin.Context) {
	var req secret.CreateRequest
	if !bindJSON(c, &req) {
		return
	}

	secret, err := h.secrets.Create(c.Request.Context(), req)
	respond(c, secret, err, http.StatusOK)
}

func (h handler) listSecrets(c *gin.Context) {
	limit, cursor := listParams(c)
	secrets, err := h.secrets.List(c.Request.Context(), limit, cursor)
	respond(c, secrets, err, http.StatusOK)
}

func (h handler) getSecretByID(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), c.Param("id"), "")
	respond(c, secret, err, http.StatusOK)
}

func (h handler) getSecretByKey(c *gin.Context) {
	secret, err := h.secrets.Get(c.Request.Context(), "", c.Param("key"))
	respond(c, secret, err, http.StatusOK)
}

func (h handler) resolveSecret(c *gin.Context) {
	var req secret.ResolveRequest
	if !bindJSON(c, &req) {
		return
	}

	value, err := h.secrets.Resolve(c.Request.Context(), req.ID, req.Key)
	respond(c, value, err, http.StatusOK)
}

func (h handler) removeSecretByID(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), c.Param("id"), "")
	respond(c, secret, err, http.StatusOK)
}

func (h handler) removeSecretByKey(c *gin.Context) {
	secret, err := h.secrets.Remove(c.Request.Context(), "", c.Param("key"))
	respond(c, secret, err, http.StatusOK)
}

func (h handler) createTemplate(c *gin.Context) {
	var req templatepkg.CreateRequest
	if !bindJSON(c, &req) {
		return
	}

	template, err := h.templates.Create(c.Request.Context(), req)
	respond(c, template, err, http.StatusOK)
}

func (h handler) listTemplates(c *gin.Context) {
	limit, cursor := listParams(c)
	templates, err := h.templates.List(c.Request.Context(), limit, cursor)
	respond(c, templates, err, http.StatusOK)
}

func (h handler) getTemplateByID(c *gin.Context) {
	template, err := h.templates.Get(c.Request.Context(), c.Param("id"), "")
	respond(c, template, err, http.StatusOK)
}

func (h handler) getTemplateByKey(c *gin.Context) {
	template, err := h.templates.Get(c.Request.Context(), "", c.Param("key"))
	respond(c, template, err, http.StatusOK)
}

func (h handler) removeTemplateByID(c *gin.Context) {
	template, err := h.templates.Remove(c.Request.Context(), c.Param("id"), "")
	respond(c, template, err, http.StatusOK)
}

func (h handler) removeTemplateByKey(c *gin.Context) {
	template, err := h.templates.Remove(c.Request.Context(), "", c.Param("key"))
	respond(c, template, err, http.StatusOK)
}

func (h handler) createSandbox(c *gin.Context) {
	var req sandbox.CreateRequest
	if !bindJSON(c, &req) {
		return
	}

	sandbox, err := h.sandboxes.Create(c.Request.Context(), req)
	respond(c, sandbox, err, http.StatusOK)
}

func (h handler) listSandboxes(c *gin.Context) {
	limit, cursor := listParams(c)
	sandboxes, err := h.sandboxes.List(c.Request.Context(), limit, cursor)
	respond(c, sandboxes, err, http.StatusOK)
}

func (h handler) getSandbox(c *gin.Context) {
	sandbox, err := h.sandboxes.Get(c.Request.Context(), c.Param("id"))
	respond(c, sandbox, err, http.StatusOK)
}

func (h handler) pauseSandbox(c *gin.Context) {
	sandbox, err := h.sandboxes.Pause(c.Request.Context(), c.Param("id"))
	respond(c, sandbox, err, http.StatusOK)
}

func (h handler) removeSandbox(c *gin.Context) {
	sandbox, err := h.sandboxes.Remove(c.Request.Context(), c.Param("id"))
	respond(c, sandbox, err, http.StatusOK)
}

func (h handler) execSandbox(c *gin.Context) {
	var req sandbox.ExecRequest
	if !bindJSON(c, &req) {
		return
	}

	response, err := h.sandboxes.Exec(c.Request.Context(), c.Param("id"), req.Command)
	respond(c, response, err, http.StatusOK)
}

func (h handler) createCheckpoint(c *gin.Context) {
	var req checkpoint.CreateRequest
	if !bindJSON(c, &req) {
		return
	}

	checkpoint, err := h.checkpoints.Create(c.Request.Context(), req)
	respond(c, checkpoint, err, http.StatusOK)
}

func (h handler) listCheckpoints(c *gin.Context) {
	limit, cursor := listParams(c)
	checkpoints, err := h.checkpoints.List(c.Request.Context(), limit, cursor)
	respond(c, checkpoints, err, http.StatusOK)
}

func (h handler) getCheckpointByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), c.Param("id"), "")
	respond(c, checkpoint, err, http.StatusOK)
}

func (h handler) getCheckpointByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Get(c.Request.Context(), "", c.Param("key"))
	respond(c, checkpoint, err, http.StatusOK)
}

func (h handler) removeCheckpointByID(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), c.Param("id"), "")
	respond(c, checkpoint, err, http.StatusOK)
}

func (h handler) removeCheckpointByKey(c *gin.Context) {
	checkpoint, err := h.checkpoints.Remove(c.Request.Context(), "", c.Param("key"))
	respond(c, checkpoint, err, http.StatusOK)
}

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}

	return true
}

func respond(c *gin.Context, value any, err error, okStatus int) {
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(okStatus, value)
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

func listParams(c *gin.Context) (int, string) {
	limit := 20

	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	return limit, c.Query("cursor")
}
