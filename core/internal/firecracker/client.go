package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the privileged bastiond Firecracker runtime API over a Unix socket.
type Client struct {
	socketPath string
	http       *http.Client
}

// NewClient returns a bastiond API client.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{
		socketPath: socketPath,
		http: &http.Client{
			Transport: transport,
			Timeout:   240 * time.Second,
		},
	}
}

// Launch asks bastiond to launch a VM.
func (c *Client) Launch(ctx context.Context, req LaunchRequest) (VM, error) {
	var vm VM
	return vm, c.do(ctx, http.MethodPost, "/v1/vms", req, &vm)
}

// State asks bastiond to reconcile a VM.
func (c *Client) State(ctx context.Context, environmentID string) (VM, error) {
	var vm VM
	return vm, c.do(ctx, http.MethodGet, "/v1/vms/"+url.PathEscape(environmentID), nil, &vm)
}

// Remove asks bastiond to stop and clean a VM.
func (c *Client) Remove(ctx context.Context, environmentID string) (VM, error) {
	var vm VM
	return vm, c.do(ctx, http.MethodDelete, "/v1/vms/"+url.PathEscape(environmentID), nil, &vm)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader

	if in != nil {
		contents, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode bastiond request: %w", err)
		}

		body = bytes.NewReader(contents)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://bastiond"+path, body)
	if err != nil {
		return fmt.Errorf("create bastiond request: %w", err)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		var apiErr struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || strings.TrimSpace(apiErr.Error) == "" {
			return fmt.Errorf("bastiond returned %s", res.Status)
		}

		return fmt.Errorf("bastiond returned %s: %s", res.Status, apiErr.Error)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode bastiond response: %w", err)
	}

	return nil
}
