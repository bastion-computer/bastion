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

	"github.com/bastion-computer/bastion/core/internal/failure"
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
		},
	}
}

// Launch asks bastiond to launch a VM.
func (c *Client) Launch(ctx context.Context, req LaunchRequest) (VM, error) {
	return c.launch(ctx, req)
}

func (c *Client) launch(ctx context.Context, launchReq LaunchRequest) (VM, error) {
	var vm VM

	contents, err := json.Marshal(launchReq)
	if err != nil {
		return vm, fmt.Errorf("encode bastiond request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bastiond/v1/vms", bytes.NewReader(contents))
	if err != nil {
		return vm, fmt.Errorf("create bastiond request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := c.http.Do(req)
	if err != nil {
		return vm, fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return vm, decodeDaemonStatusError(res)
	}

	decoder := json.NewDecoder(res.Body)
	for {
		var event LaunchStreamEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				return vm, fmt.Errorf("bastiond stream ended before VM launch completed")
			}

			return vm, fmt.Errorf("decode bastiond stream: %w", err)
		}

		switch event.Type {
		case StreamEventLog:
			if launchReq.Logs == nil || event.Log == "" {
				continue
			}

			if _, err := launchReq.Logs.Write([]byte(event.Log)); err != nil {
				return vm, fmt.Errorf("stream VM init logs: %w", err)
			}
		case StreamEventResult:
			if event.VM == nil {
				return vm, fmt.Errorf("bastiond stream result missing VM")
			}

			return *event.VM, nil
		case StreamEventError:
			if event.VM != nil {
				vm = *event.VM
			}

			status := event.Status
			if status == 0 {
				status = http.StatusInternalServerError
			}

			message := strings.TrimSpace(event.Error)
			if message == "" {
				message = "unknown error"
			}

			return vm, daemonStatusError(status, "bastiond returned %s: %s", httpStatus(status), message)
		default:
			return vm, fmt.Errorf("bastiond stream returned unknown event type %q", event.Type)
		}
	}
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
		return decodeDaemonStatusError(res)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode bastiond response: %w", err)
	}

	return nil
}

func decodeDaemonStatusError(res *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || strings.TrimSpace(apiErr.Error) == "" {
		return daemonStatusError(res.StatusCode, "bastiond returned %s", res.Status)
	}

	return daemonStatusError(res.StatusCode, "bastiond returned %s: %s", res.Status, apiErr.Error)
}

func daemonStatusError(statusCode int, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if statusCode == http.StatusFailedDependency {
		return fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	}

	return err
}

func httpStatus(status int) string {
	if text := http.StatusText(status); text != "" {
		return fmt.Sprintf("%d %s", status, text)
	}

	return fmt.Sprintf("%d", status)
}
