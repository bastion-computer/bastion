// Package client calls the local Bastion HTTP API.
package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
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
		http:    &http.Client{},
	}
}

// CreateTemplate stores a template.
func (c *Client) CreateTemplate(ctx context.Context, req template.CreateRequest) (template.Metadata, error) {
	return postHostStream(ctx, c.http, c.baseURL+"/v1/templates", req, req.Logs, decodeCreateTemplateStream)
}

func decodeCreateTemplateStream(decoder *json.Decoder, logs io.Writer) (template.Metadata, error) {
	var out template.Metadata

	for {
		var event template.CreateStreamEvent
		if err := decoder.Decode(&event); err != nil {
			return out, createTemplateStreamDecodeError(err)
		}

		created, done, err := handleCreateTemplateStreamEvent(event, logs)
		if done || err != nil {
			return created, err
		}
	}
}

func createTemplateStreamDecodeError(err error) error {
	return hostStreamDecodeError(err, "template creation")
}

func handleCreateTemplateStreamEvent(event template.CreateStreamEvent, logs io.Writer) (template.Metadata, bool, error) {
	return handleHostStreamEvent(event.Type, event.Log, event.Error, event.Status, event.Template, "template", "template init", logs)
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
func (c *Client) CreateEnvironment(ctx context.Context, createReq environment.CreateRequest) (environment.Environment, error) {
	return postHostStream(ctx, c.http, c.baseURL+"/v1/environments", createReq, createReq.Logs, decodeCreateEnvironmentStream)
}

func decodeCreateEnvironmentStream(decoder *json.Decoder, logs io.Writer) (environment.Environment, error) {
	var out environment.Environment

	for {
		var event environment.CreateStreamEvent
		if err := decoder.Decode(&event); err != nil {
			return out, createEnvironmentStreamDecodeError(err)
		}

		created, done, err := handleCreateEnvironmentStreamEvent(event, logs)
		if done || err != nil {
			return created, err
		}
	}
}

func createEnvironmentStreamDecodeError(err error) error {
	return hostStreamDecodeError(err, "environment creation")
}

func handleCreateEnvironmentStreamEvent(event environment.CreateStreamEvent, logs io.Writer) (environment.Environment, bool, error) {
	return handleHostStreamEvent(event.Type, event.Log, event.Error, event.Status, event.Environment, "environment", "environment", logs)
}

func postHostStream[T any](ctx context.Context, client *http.Client, target string, in any, logs io.Writer, decode func(*json.Decoder, io.Writer) (T, error)) (T, error) {
	var out T

	contents, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(contents))
	if err != nil {
		return out, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := client.Do(req)
	if err != nil {
		return out, fmt.Errorf("call host API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeHostStatusError(res)
	}

	return decode(json.NewDecoder(res.Body), logs)
}

func hostStreamDecodeError(err error, operation string) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("host API stream ended before %s completed", operation)
	}

	return fmt.Errorf("decode host API stream: %w", err)
}

func handleHostStreamEvent[T any](eventType, logText, errorText string, status int, result *T, resultName, logLabel string, logs io.Writer) (T, bool, error) {
	var out T

	switch eventType {
	case template.StreamEventLog:
		if logs == nil || logText == "" {
			return out, false, nil
		}

		if _, err := logs.Write([]byte(logText)); err != nil {
			return out, false, fmt.Errorf("stream %s logs: %w", logLabel, err)
		}

		return out, false, nil
	case template.StreamEventResult:
		if result == nil {
			return out, false, fmt.Errorf("host API stream result missing %s", resultName)
		}

		return *result, true, nil
	case template.StreamEventError:
		if status == 0 {
			status = http.StatusInternalServerError
		}

		message := strings.TrimSpace(errorText)
		if message == "" {
			message = "unknown error"
		}

		return out, false, fmt.Errorf("host API returned %s: %s", httpStatus(status), message)
	default:
		return out, false, fmt.Errorf("host API stream returned unknown event type %q", eventType)
	}
}

// ListEnvironments returns environments.
func (c *Client) ListEnvironments(ctx context.Context, limit int, cursor string, tags []string) (services.Page[environment.Environment], error) {
	var out services.Page[environment.Environment]
	return out, c.do(ctx, http.MethodGet, listPath("/v1/environments", limit, cursor, tags...), nil, &out)
}

// GetEnvironment returns an environment by ID.
func (c *Client) GetEnvironment(ctx context.Context, id string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(id), nil, &out)
}

// GetEnvironmentByKey returns an environment by key.
func (c *Client) GetEnvironmentByKey(ctx context.Context, key string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodGet, "/v1/environments/by-key/"+url.PathEscape(key), nil, &out)
}

// RemoveEnvironment deletes an environment.
func (c *Client) RemoveEnvironment(ctx context.Context, id string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodDelete, "/v1/environments/"+url.PathEscape(id), nil, &out)
}

// RemoveEnvironmentByKey deletes an environment by key.
func (c *Client) RemoveEnvironmentByKey(ctx context.Context, key string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.do(ctx, http.MethodDelete, "/v1/environments/by-key/"+url.PathEscape(key), nil, &out)
}

// GetEnvironmentTunnels returns registered tunnels for an environment by ID or key.
func (c *Client) GetEnvironmentTunnels(ctx context.Context, id, key string) (environment.Tunnels, error) {
	var out environment.Tunnels

	path, err := resourcePath("/v1/environments", id, key)
	if err != nil {
		return out, err
	}

	return out, c.do(ctx, http.MethodGet, path+"/tunnels", nil, &out)
}

// OpenSSH opens an upgraded API SSH tunnel for an environment.
func (c *Client) OpenSSH(ctx context.Context, id string, tunnelReq sshtunnel.Request) (io.ReadWriteCloser, error) {
	contents, err := json.Marshal(tunnelReq)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	target, err := url.Parse(c.baseURL + "/v1/environments/" + url.PathEscape(id) + "/ssh")
	if err != nil {
		return nil, fmt.Errorf("parse host API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", sshtunnel.Protocol)

	conn, err := dialHTTP(ctx, target)
	if err != nil {
		return nil, err
	}

	if err := req.Write(conn); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("write SSH upgrade request: %w", err)
	}

	reader := bufio.NewReader(conn)

	res, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("read SSH upgrade response: %w", err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = res.Body.Close() }()
		defer func() { _ = conn.Close() }()

		if res.StatusCode >= http.StatusBadRequest {
			return nil, decodeHostStatusError(res)
		}

		return nil, fmt.Errorf("host API returned %s, want %d Switching Protocols", res.Status, http.StatusSwitchingProtocols)
	}

	if !strings.EqualFold(res.Header.Get("Upgrade"), sshtunnel.Protocol) {
		_ = conn.Close()

		return nil, fmt.Errorf("host API returned unexpected SSH upgrade protocol %q", res.Header.Get("Upgrade"))
	}

	return &upgradedConn{Conn: conn, reader: reader}, nil
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
		return decodeHostStatusError(res)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func decodeHostStatusError(res *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || apiErr.Error == "" {
		return fmt.Errorf("host API returned %s", res.Status)
	}

	return fmt.Errorf("host API returned %s: %s", res.Status, apiErr.Error)
}

func httpStatus(status int) string {
	if text := http.StatusText(status); text != "" {
		return fmt.Sprintf("%d %s", status, text)
	}

	return strconv.Itoa(status)
}

func listPath(path string, limit int, cursor string, tags ...string) string {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}

	if cursor != "" {
		values.Set("cursor", cursor)
	}

	for _, tag := range tags {
		values.Add("tag", tag)
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

func dialHTTP(ctx context.Context, target *url.URL) (net.Conn, error) {
	host := target.Hostname()

	port := target.Port()
	if port == "" {
		switch target.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}

	addr := net.JoinHostPort(host, port)

	dialer := net.Dialer{}

	switch target.Scheme {
	case "http":
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial host API: %w", err)
		}

		return conn, nil
	case "https":
		tlsDialer := tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}

		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial host API: %w", err)
		}

		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported host API scheme %q", target.Scheme)
	}
}

type upgradedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *upgradedConn) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}

	return c.Conn.Read(p)
}
