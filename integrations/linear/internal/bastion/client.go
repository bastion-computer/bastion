// Package bastion contains the small Bastion API client used by the Linear integration.
//
//nolint:wsl_v5 // Protocol client code keeps request/response steps adjacent.
package bastion

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

	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

// Environment is the public environment response shape returned by Bastion.
type Environment struct {
	ID         string   `json:"id"`
	Key        *string  `json:"key,omitempty"`
	Status     string   `json:"status"`
	TemplateID string   `json:"templateId"`
	Tags       []string `json:"tags"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	LastError  string   `json:"lastError,omitempty"`
}

// Page is a Bastion list response.
type Page[T any] struct {
	Cursor  *string `json:"cursor"`
	Entries []T     `json:"entries"`
}

// CommandResult is the result of running a command through the Bastion SSH API.
type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Client calls the Bastion host API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Bastion API client.
func NewClient(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}
}

// ListEnvironments returns all environments matching the given tag filters.
func (c *Client) ListEnvironments(ctx context.Context, tags []string) ([]Environment, error) {
	var out []Environment
	cursor := ""

	for {
		page, err := c.listEnvironmentPage(ctx, cursor, tags)
		if err != nil {
			return nil, err
		}

		out = append(out, page.Entries...)
		if page.Cursor == nil || *page.Cursor == "" {
			return out, nil
		}

		cursor = *page.Cursor
	}
}

// GetEnvironment returns one environment by ID.
func (c *Client) GetEnvironment(ctx context.Context, id string) (Environment, error) {
	var out Environment
	return out, c.do(ctx, http.MethodGet, "/v1/environments/"+url.PathEscape(id), nil, &out)
}

func (c *Client) listEnvironmentPage(ctx context.Context, cursor string, tags []string) (Page[Environment], error) {
	values := url.Values{}
	values.Set("limit", "100")
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	for _, tag := range tags {
		values.Add("tag", tag)
	}

	var out Page[Environment]
	return out, c.do(ctx, http.MethodGet, "/v1/environments?"+values.Encode(), nil, &out)
}

// RunCommand runs a command in an environment over the Bastion SSH API.
func (c *Client) RunCommand(ctx context.Context, environmentID string, command []string, stdin []byte) (CommandResult, error) {
	stream, err := c.OpenSSH(ctx, environmentID, sshtunnel.Request{Command: command})
	if err != nil {
		return CommandResult{}, err
	}
	defer func() { _ = stream.Close() }()

	writer := sshtunnel.NewFrameWriter(stream)
	if len(stdin) > 0 {
		if err := writer.WriteFrame(sshtunnel.FrameStdin, stdin); err != nil {
			return CommandResult{}, fmt.Errorf("write SSH stdin: %w", err)
		}
	}
	if err := writer.WriteFrame(sshtunnel.FrameStdinEOF, nil); err != nil {
		return CommandResult{}, fmt.Errorf("write SSH stdin EOF: %w", err)
	}

	var result CommandResult
	for {
		frameType, payload, err := sshtunnel.ReadFrame(stream)
		if err != nil {
			return result, fmt.Errorf("read SSH frame: %w", err)
		}

		switch frameType {
		case sshtunnel.FrameStdout:
			result.Stdout = append(result.Stdout, payload...)
		case sshtunnel.FrameStderr:
			result.Stderr = append(result.Stderr, payload...)
		case sshtunnel.FrameError:
			return result, fmt.Errorf("bastion SSH error: %s", string(payload))
		case sshtunnel.FrameExit:
			var status sshtunnel.ExitStatus
			if err := json.Unmarshal(payload, &status); err != nil {
				return result, fmt.Errorf("decode SSH exit status: %w", err)
			}
			result.ExitCode = status.Code
			return result, nil
		}
	}
}

// RunShell runs a POSIX shell script through the Bastion SSH API.
func (c *Client) RunShell(ctx context.Context, environmentID, script string, stdin []byte) (CommandResult, error) {
	return c.RunCommand(ctx, environmentID, []string{"sh", "-lc", ShellQuote(script)}, stdin)
}

// ShellQuote quotes one POSIX shell argument.
func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// OpenSSH opens an upgraded SSH stream to an environment.
func (c *Client) OpenSSH(ctx context.Context, id string, tunnelReq sshtunnel.Request) (io.ReadWriteCloser, error) {
	contents, err := json.Marshal(tunnelReq)
	if err != nil {
		return nil, fmt.Errorf("encode SSH request: %w", err)
	}

	target, err := url.Parse(c.baseURL + "/v1/environments/" + url.PathEscape(id) + "/ssh")
	if err != nil {
		return nil, fmt.Errorf("parse Bastion API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("create SSH request: %w", err)
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
			return nil, decodeAPIError(res)
		}
		return nil, fmt.Errorf("bastion API returned %s, want 101 Switching Protocols", res.Status)
	}

	if !strings.EqualFold(res.Header.Get("Upgrade"), sshtunnel.Protocol) {
		_ = conn.Close()
		return nil, fmt.Errorf("bastion API returned unexpected SSH upgrade protocol %q", res.Header.Get("Upgrade"))
	}

	return &upgradedConn{Conn: conn, reader: reader}, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		contents, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode Bastion API request: %w", err)
		}
		body = bytes.NewReader(contents)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create Bastion API request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Bastion API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeAPIError(res)
	}
	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Bastion API response: %w", err)
	}

	return nil
}

func decodeAPIError(res *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || apiErr.Error == "" {
		return fmt.Errorf("bastion API returned %s", res.Status)
	}

	return fmt.Errorf("bastion API returned %s: %s", res.Status, apiErr.Error)
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
	if host == "" || port == "" {
		return nil, fmt.Errorf("invalid Bastion API URL %q", target.String())
	}

	dialer := net.Dialer{}
	addr := net.JoinHostPort(host, port)
	switch target.Scheme {
	case "http":
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial Bastion API: %w", err)
		}
		return conn, nil
	case "https":
		conn, err := (&tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial Bastion API: %w", err)
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported Bastion API scheme %q", target.Scheme)
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

// ExitError reports a remote non-zero exit status.
type ExitError struct {
	Code   int
	Stderr string
}

func (e ExitError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return "remote command exited with status " + strconv.Itoa(e.Code)
	}

	return "remote command exited with status " + strconv.Itoa(e.Code) + ": " + stderr
}

// CheckExit returns an ExitError for non-zero command results.
func CheckExit(result CommandResult) error {
	if result.ExitCode == 0 {
		return nil
	}

	return ExitError{Code: result.ExitCode, Stderr: string(result.Stderr)}
}

// IsExitError reports whether err is an ExitError.
func IsExitError(err error) bool {
	var exitErr ExitError
	return errors.As(err, &exitErr)
}
