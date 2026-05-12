// Package client calls the local Bastion HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

// Client wraps HTTP access to the Bastion API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Bastion API client for baseURL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateSecret binds a secret reference.
func (c *Client) CreateSecret(ctx context.Context, req secret.CreateRequest) (secret.Secret, error) {
	var out secret.Secret
	return out, c.do(ctx, http.MethodPost, "/v1/secrets", req, &out)
}

// ListSecrets returns secret references.
func (c *Client) ListSecrets(ctx context.Context, limit int, cursor string) (services.Page[secret.Secret], error) {
	var out services.Page[secret.Secret]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/secrets", limit, cursor), nil, &out)
}

// GetSecret returns a secret reference by ID or key.
func (c *Client) GetSecret(ctx context.Context, id, key string) (secret.Secret, error) {
	var out secret.Secret

	path, err := resourcePath("/v1/secrets", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// ResolveSecret returns the host environment value for a secret reference.
func (c *Client) ResolveSecret(ctx context.Context, id, key string) (secret.Value, error) {
	var out secret.Value

	req := secret.ResolveRequest{ID: id, Key: key}

	return out, c.do(ctx, http.MethodPost, "/v1/secrets/resolve", req, &out)
}

// RemoveSecret deletes a secret reference.
func (c *Client) RemoveSecret(ctx context.Context, id, key string) (secret.Secret, error) {
	var out secret.Secret

	path, err := resourcePath("/v1/secrets", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodDelete, path, nil, &out)
}

// CreateTemplate stores a sandbox template.
func (c *Client) CreateTemplate(ctx context.Context, req template.CreateRequest) (template.Metadata, error) {
	var out template.Metadata
	return out, c.do(ctx, http.MethodPost, "/v1/templates", req, &out)
}

// ListTemplates returns template metadata.
func (c *Client) ListTemplates(ctx context.Context, limit int, cursor string) (services.Page[template.Metadata], error) {
	var out services.Page[template.Metadata]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/templates", limit, cursor), nil, &out)
}

// GetTemplate returns a template by ID or key.
func (c *Client) GetTemplate(ctx context.Context, id, key string) (template.Template, error) {
	var out template.Template

	path, err := resourcePath("/v1/templates", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// RemoveTemplate deletes a template.
func (c *Client) RemoveTemplate(ctx context.Context, id, key string) (template.Template, error) {
	var out template.Template

	path, err := resourcePath("/v1/templates", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodDelete, path, nil, &out)
}

// CreateSandbox creates a sandbox from a template or checkpoint.
func (c *Client) CreateSandbox(ctx context.Context, req sandbox.CreateRequest) (sandbox.Sandbox, error) {
	var out sandbox.Sandbox
	return out, c.do(ctx, http.MethodPost, "/v1/sandboxes", req, &out)
}

// ListSandboxes returns sandboxes.
func (c *Client) ListSandboxes(ctx context.Context, limit int, cursor string) (services.Page[sandbox.Sandbox], error) {
	var out services.Page[sandbox.Sandbox]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/sandboxes", limit, cursor), nil, &out)
}

// GetSandbox returns a sandbox by ID.
func (c *Client) GetSandbox(ctx context.Context, id string) (sandbox.Sandbox, error) {
	var out sandbox.Sandbox
	return out, c.do(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(id), nil, &out)
}

// PauseSandbox marks a sandbox as paused.
func (c *Client) PauseSandbox(ctx context.Context, id string) (sandbox.Sandbox, error) {
	var out sandbox.Sandbox
	return out, c.do(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/pause", nil, &out)
}

// RemoveSandbox deletes a sandbox.
func (c *Client) RemoveSandbox(ctx context.Context, id string) (sandbox.Sandbox, error) {
	var out sandbox.Sandbox
	return out, c.do(ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(id), nil, &out)
}

// ExecSandbox requests command execution in a sandbox.
func (c *Client) ExecSandbox(ctx context.Context, id string, command []string) (sandbox.ExecResponse, error) {
	var out sandbox.ExecResponse

	req := sandbox.ExecRequest{Command: command}

	return out, c.do(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/exec", req, &out)
}

// CreateCheckpoint creates a checkpoint from a paused sandbox.
func (c *Client) CreateCheckpoint(ctx context.Context, req checkpoint.CreateRequest) (checkpoint.Checkpoint, error) {
	var out checkpoint.Checkpoint
	return out, c.do(ctx, http.MethodPost, "/v1/checkpoints", req, &out)
}

// ListCheckpoints returns checkpoints.
func (c *Client) ListCheckpoints(ctx context.Context, limit int, cursor string) (services.Page[checkpoint.Checkpoint], error) {
	var out services.Page[checkpoint.Checkpoint]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/checkpoints", limit, cursor), nil, &out)
}

// GetCheckpoint returns a checkpoint by ID or key.
func (c *Client) GetCheckpoint(ctx context.Context, id, key string) (checkpoint.Checkpoint, error) {
	var out checkpoint.Checkpoint

	path, err := resourcePath("/v1/checkpoints", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// RemoveCheckpoint deletes a checkpoint.
func (c *Client) RemoveCheckpoint(ctx context.Context, id, key string) (checkpoint.Checkpoint, error) {
	var out checkpoint.Checkpoint

	path, err := resourcePath("/v1/checkpoints", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodDelete, path, nil, &out)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader

	if in != nil {
		contents, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}

		body = bytes.NewReader(contents)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call host API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || apiErr.Error == "" {
			return fmt.Errorf("host API returned %s", res.Status)
		}

		return fmt.Errorf("host API returned %s: %s", res.Status, apiErr.Error)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func listPath(path string, limit int, cursor string) string {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}

	if cursor != "" {
		values.Set("cursor", cursor)
	}

	if len(values) == 0 {
		return path
	}

	return path + "?" + values.Encode()
}

func resourcePath(path, id, key string) (string, error) {
	switch {
	case id != "" && key == "":
		return path + "/" + url.PathEscape(id), nil
	case id == "" && key != "":
		return path + "/by-key/" + url.PathEscape(key), nil
	default:
		return "", errors.New("specify exactly one of id or key")
	}
}
