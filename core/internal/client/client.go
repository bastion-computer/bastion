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
	"github.com/bastion-computer/bastion/core/internal/services/environment"
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

// CreateTemplate stores a template.
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

// CreateEnvironment creates an environment from a template.
func (c *Client) CreateEnvironment(ctx context.Context, req environment.CreateRequest) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodPost, "/v1/environments", req, &out)
}

// ListEnvironments returns environments.
func (c *Client) ListEnvironments(ctx context.Context, limit int, cursor string) (services.Page[environment.Environment], error) {
	var out services.Page[environment.Environment]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/environments", limit, cursor), nil, &out)
}

// GetEnvironment returns an environment by ID.
func (c *Client) GetEnvironment(ctx context.Context, id string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(id), nil, &out)
}

// RemoveEnvironment deletes an environment.
func (c *Client) RemoveEnvironment(ctx context.Context, id string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodDelete, "/v1/environments/"+url.PathEscape(id), nil, &out)
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
