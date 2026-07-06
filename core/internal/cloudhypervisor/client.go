package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
	"github.com/bastion-computer/bastion/core/internal/failure"
)

// Client calls the privileged bastiond runtime API over a Unix socket.
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

// BuildBase asks bastiond to build and snapshot the base image.
func (c *Client) BuildBase(ctx context.Context, buildReq BuildBaseRequest) (basearchive.Metadata, error) {
	path := "/v1/base/build"
	if buildReq.Force {
		path += "?force=true"
	}

	return postDaemonStream(ctx, c, path, buildReq, buildReq.Logs, decodeBaseStream)
}

// GetBase asks bastiond for current base metadata.
func (c *Client) GetBase(ctx context.Context) (basearchive.Metadata, error) {
	var base basearchive.Metadata
	return base, c.do(ctx, http.MethodGet, "/v1/base", nil, &base)
}

// ExportBase asks bastiond to stream base artifacts.
func (c *Client) ExportBase(ctx context.Context, exportReq ExportBaseRequest) error {
	if exportReq.Writer == nil {
		return errors.New("base archive writer is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://bastiond/v1/base/export", nil)
	if err != nil {
		return fmt.Errorf("create bastiond request: %w", err)
	}

	req.Header.Set("Accept", BaseArchiveContentType)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeDaemonStatusError(res)
	}

	if _, err := io.Copy(exportReq.Writer, res.Body); err != nil {
		return fmt.Errorf("read bastiond base archive: %w", err)
	}

	return nil
}

// ImportBase asks bastiond to restore base artifacts from an archive.
func (c *Client) ImportBase(ctx context.Context, importReq ImportBaseRequest) (basearchive.Metadata, error) {
	if importReq.Reader == nil {
		return basearchive.Metadata{}, errors.New("base archive reader is required")
	}

	path := "/v1/base/import"
	if importReq.Force {
		path += "?force=true"
	}

	return postDaemonStreamBody(ctx, c, path, importReq.Reader, BaseArchiveContentType, importReq.ContentLength, importReq.Logs, decodeBaseStream)
}

func decodeBaseStream(decoder *json.Decoder, logs io.Writer) (basearchive.Metadata, error) {
	var base basearchive.Metadata

	for {
		var event BaseStreamEvent
		if err := decoder.Decode(&event); err != nil {
			return base, daemonStreamDecodeError(err, "base operation")
		}

		decoded, done, err := handleDaemonStreamEvent(event.Type, event.Log, event.Error, event.Status, event.Base, "base", "base", logs)
		if done || err != nil {
			return decoded, err
		}
	}
}

// PrepareTemplate asks bastiond to prepare and snapshot a template VM.
func (c *Client) PrepareTemplate(ctx context.Context, prepareReq PrepareTemplateRequest) (PreparedTemplate, error) {
	return postDaemonStream(ctx, c, "/v1/templates", prepareReq, prepareReq.Logs, decodePrepareTemplateStream)
}

// RemoveTemplate asks bastiond to delete prepared template artifacts.
func (c *Client) RemoveTemplate(ctx context.Context, templateID string) (PreparedTemplate, error) {
	var prepared PreparedTemplate
	return prepared, c.do(ctx, http.MethodDelete, "/v1/templates/"+url.PathEscape(templateID), nil, &prepared)
}

// ExportTemplate asks bastiond to stream prepared template artifacts.
func (c *Client) ExportTemplate(ctx context.Context, exportReq ExportTemplateRequest) error {
	if exportReq.Writer == nil {
		return errors.New("template archive writer is required")
	}

	contents, err := json.Marshal(exportReq)
	if err != nil {
		return fmt.Errorf("encode bastiond request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bastiond/v1/templates/export", bytes.NewReader(contents))
	if err != nil {
		return fmt.Errorf("create bastiond request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", TemplateArchiveContentType)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeDaemonStatusError(res)
	}

	if _, err := io.Copy(exportReq.Writer, res.Body); err != nil {
		return fmt.Errorf("read bastiond template archive: %w", err)
	}

	return nil
}

// ImportTemplate asks bastiond to restore prepared template artifacts from an archive.
func (c *Client) ImportTemplate(ctx context.Context, importReq ImportTemplateRequest) (ImportedTemplate, error) {
	var imported ImportedTemplate

	if strings.TrimSpace(importReq.TemplateID) == "" {
		return imported, errors.New("template id is required")
	}

	if importReq.Reader == nil {
		return imported, errors.New("template archive reader is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bastiond/v1/templates/"+url.PathEscape(importReq.TemplateID)+"/import", importReq.Reader)
	if err != nil {
		return imported, fmt.Errorf("create bastiond request: %w", err)
	}

	req.Header.Set("Content-Type", TemplateArchiveContentType)

	if importReq.ContentLength > 0 {
		req.ContentLength = importReq.ContentLength
	}

	res, err := c.http.Do(req)
	if err != nil {
		return imported, fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return imported, decodeDaemonStatusError(res)
	}

	if err := json.NewDecoder(res.Body).Decode(&imported); err != nil {
		return imported, fmt.Errorf("decode bastiond response: %w", err)
	}

	return imported, nil
}

func decodePrepareTemplateStream(decoder *json.Decoder, logs io.Writer) (PreparedTemplate, error) {
	var prepared PreparedTemplate

	for {
		var event PrepareTemplateStreamEvent
		if err := decoder.Decode(&event); err != nil {
			return prepared, prepareTemplateStreamDecodeError(err)
		}

		decoded, done, err := handlePrepareTemplateStreamEvent(event, logs)
		if done || err != nil {
			return decoded, err
		}
	}
}

func prepareTemplateStreamDecodeError(err error) error {
	return daemonStreamDecodeError(err, "template preparation")
}

func handlePrepareTemplateStreamEvent(event PrepareTemplateStreamEvent, logs io.Writer) (PreparedTemplate, bool, error) {
	return handleDaemonStreamEvent(event.Type, event.Log, event.Error, event.Status, event.Template, "template", "template init", logs)
}

// Launch asks bastiond to launch a VM.
func (c *Client) Launch(ctx context.Context, launchReq LaunchRequest) (VM, error) {
	return postDaemonStream(ctx, c, "/v1/vms", launchReq, launchReq.Logs, decodeLaunchStream)
}

func decodeLaunchStream(decoder *json.Decoder, logs io.Writer) (VM, error) {
	var vm VM

	for {
		var event LaunchStreamEvent
		if err := decoder.Decode(&event); err != nil {
			return vm, launchStreamDecodeError(err)
		}

		decoded, done, err := handleLaunchStreamEvent(event, logs)
		if done || err != nil {
			return decoded, err
		}
	}
}

func launchStreamDecodeError(err error) error {
	return daemonStreamDecodeError(err, "VM launch")
}

func handleLaunchStreamEvent(event LaunchStreamEvent, logs io.Writer) (VM, bool, error) {
	return handleDaemonStreamEvent(event.Type, event.Log, event.Error, event.Status, event.VM, "VM", "VM init", logs)
}

func postDaemonStream[T any](ctx context.Context, c *Client, path string, in any, logs io.Writer, decode func(*json.Decoder, io.Writer) (T, error)) (T, error) {
	var out T

	contents, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode bastiond request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bastiond"+path, bytes.NewReader(contents))
	if err != nil {
		return out, fmt.Errorf("create bastiond request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeDaemonStatusError(res)
	}

	return decode(json.NewDecoder(res.Body), logs)
}

func postDaemonStreamBody[T any](ctx context.Context, c *Client, path string, body io.Reader, contentType string, contentLength int64, logs io.Writer, decode func(*json.Decoder, io.Writer) (T, error)) (T, error) {
	var out T

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://bastiond"+path, body)
	if err != nil {
		return out, fmt.Errorf("create bastiond request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	req.Header.Set("Accept", "application/x-ndjson")

	if contentLength > 0 {
		req.ContentLength = contentLength
	}

	res, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("call bastiond at %s: %w", c.socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeDaemonStatusError(res)
	}

	return decode(json.NewDecoder(res.Body), logs)
}

func daemonStreamDecodeError(err error, operation string) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("bastiond stream ended before %s completed", operation)
	}

	return fmt.Errorf("decode bastiond stream: %w", err)
}

func handleDaemonStreamEvent[T any](eventType, logText, errorText string, status int, result *T, resultName, logLabel string, logs io.Writer) (T, bool, error) {
	var out T

	switch eventType {
	case StreamEventLog:
		if logs == nil || logText == "" {
			return out, false, nil
		}

		if _, err := logs.Write([]byte(logText)); err != nil {
			return out, false, fmt.Errorf("stream %s logs: %w", logLabel, err)
		}

		return out, false, nil
	case StreamEventResult:
		if result == nil {
			return out, false, fmt.Errorf("bastiond stream result missing %s", resultName)
		}

		return *result, true, nil
	case StreamEventError:
		if result != nil {
			out = *result
		}

		if status == 0 {
			status = http.StatusInternalServerError
		}

		message := strings.TrimSpace(errorText)
		if message == "" {
			message = "unknown error"
		}

		return out, false, daemonStatusError(status, "bastiond returned %s: %s", httpStatus(status), message)
	default:
		return out, false, fmt.Errorf("bastiond stream returned unknown event type %q", eventType)
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

	switch statusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %w", failure.ErrInvalid, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", ErrBaseNotFound, err)
	case http.StatusConflict:
		return fmt.Errorf("%w: %w", ErrBaseExists, err)
	case http.StatusFailedDependency:
		return fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	}

	return err
}

func httpStatus(status int) string {
	if text := http.StatusText(status); text != "" {
		return fmt.Sprintf("%d %s", status, text)
	}

	return strconv.Itoa(status)
}
