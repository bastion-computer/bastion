// Package opencode adapts OpenCode server calls over Bastion SSH commands.
//
//nolint:wsl_v5 // Remote shell and HTTP request construction is easier to audit when compact.
package opencode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
)

const (
	pidFile = "/tmp/bastion-linear-opencode.pid"
	logFile = "/tmp/bastion-linear-opencode.log"
)

// Runner runs commands in Bastion environments.
type Runner interface {
	RunShell(context.Context, string, string, []byte) (bastion.CommandResult, error)
}

// Config configures OpenCode server usage.
type Config struct {
	Port      int
	Directory string
	Agent     string
	Provider  string
	Model     string
}

// Client manages OpenCode inside one environment.
type Client struct {
	runner Runner
	cfg    Config
}

// Session is an OpenCode session reference.
type Session struct {
	ID string
}

// Response is the agent response from OpenCode.
type Response struct {
	Text string
}

// NewClient returns an OpenCode adapter.
func NewClient(runner Runner, cfg Config) *Client {
	if cfg.Port == 0 {
		cfg.Port = 4096
	}

	return &Client{runner: runner, cfg: cfg}
}

// StartServer launches opencode serve in the environment if needed.
func (c *Client) StartServer(ctx context.Context, environmentID string) (int, error) {
	script := fmt.Sprintf(`set -eu
if ! command -v opencode >/dev/null 2>&1; then
  printf 'opencode is not installed\n' >&2
  exit 127
fi
if command -v curl >/dev/null 2>&1 && curl -fsS --max-time 2 http://127.0.0.1:%[1]d/global/health >/dev/null 2>&1; then
  if [ -s %[2]s ]; then cat %[2]s; else printf '0\n'; fi
  exit 0
fi
if [ -s %[2]s ] && kill -0 "$(cat %[2]s)" >/dev/null 2>&1; then
  kill "$(cat %[2]s)" >/dev/null 2>&1 || true
fi
nohup opencode serve --hostname 127.0.0.1 --port %[1]d >%[3]s 2>&1 &
pid=$!
printf '%%s\n' "$pid" >%[2]s
for attempt in $(seq 1 60); do
  if curl -fsS --max-time 2 http://127.0.0.1:%[1]d/global/health >/dev/null 2>&1; then
    printf '%%s\n' "$pid"
    exit 0
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    cat %[3]s >&2 || true
    exit 1
  fi
  sleep 1
done
cat %[3]s >&2 || true
exit 1
`, c.cfg.Port, pidFile, logFile)

	result, err := c.runner.RunShell(ctx, environmentID, script, nil)
	if err != nil {
		return 0, err
	}
	if err := bastion.CheckExit(result); err != nil {
		return 0, err
	}

	pidText := strings.TrimSpace(string(result.Stdout))
	var pid int
	if _, err := fmt.Sscanf(pidText, "%d", &pid); err != nil {
		return 0, fmt.Errorf("parse opencode pid %q: %w", pidText, err)
	}

	return pid, nil
}

// StopServer stops the tracked OpenCode server process if it is still running.
func (c *Client) StopServer(ctx context.Context, environmentID string) error {
	script := fmt.Sprintf(`set -eu
if [ -s %[1]s ]; then
  pid="$(cat %[1]s)"
  kill "$pid" >/dev/null 2>&1 || true
  rm -f %[1]s
fi
`, pidFile)

	result, err := c.runner.RunShell(ctx, environmentID, script, nil)
	if err != nil {
		return err
	}

	return bastion.CheckExit(result)
}

// CreateSession creates an OpenCode session.
func (c *Client) CreateSession(ctx context.Context, environmentID, title string) (Session, error) {
	body := map[string]any{"title": title}
	contents, err := c.request(ctx, environmentID, "POST", "/session", body)
	if err != nil {
		return Session{}, err
	}

	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(contents, &session); err != nil {
		return Session{}, fmt.Errorf("decode opencode session: %w", err)
	}
	if session.ID == "" {
		return Session{}, errors.New("opencode session response missing id")
	}

	return Session{ID: session.ID}, nil
}

// SendMessage sends a prompt and waits for OpenCode's response.
func (c *Client) SendMessage(ctx context.Context, environmentID, sessionID, prompt string, attachments []linear.Attachment) (Response, error) {
	parts := []map[string]any{{"type": "text", "text": prompt}}
	for _, attachment := range attachments {
		if attachment.URL == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"type":     "file",
			"mime":     attachmentMime(attachment),
			"filename": attachmentFilename(attachment),
			"url":      attachment.URL,
		})
	}

	body := map[string]any{"parts": parts}
	if c.cfg.Agent != "" {
		body["agent"] = c.cfg.Agent
	}
	if c.cfg.Provider != "" && c.cfg.Model != "" {
		body["model"] = map[string]any{"providerID": c.cfg.Provider, "modelID": c.cfg.Model}
	}

	contents, err := c.request(ctx, environmentID, "POST", "/session/"+sessionID+"/message", body)
	if err != nil {
		return Response{}, err
	}

	text := responseText(contents)
	if text == "" {
		text = "OpenCode completed the requested work."
	}

	return Response{Text: text}, nil
}

// Abort stops a running OpenCode session.
func (c *Client) Abort(ctx context.Context, environmentID, sessionID string) error {
	_, err := c.request(ctx, environmentID, "POST", "/session/"+sessionID+"/abort", nil)
	return err
}

func (c *Client) request(ctx context.Context, environmentID, method, path string, body any) ([]byte, error) {
	var bodyJSON []byte
	if body != nil {
		contents, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode opencode request: %w", err)
		}
		bodyJSON = contents
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", c.cfg.Port, path)
	script := remoteCurl(method, url, bodyJSON, c.cfg.Directory)
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	result, err := c.runner.RunShell(requestCtx, environmentID, script, nil)
	if err != nil {
		return nil, err
	}
	if err := bastion.CheckExit(result); err != nil {
		return nil, err
	}

	return result.Stdout, nil
}

func remoteCurl(method, url string, body []byte, directory string) string {
	args := []string{"curl", "-fsS", "--max-time", "1800", "-X", method, "-H", "Content-Type: application/json"}
	if len(body) > 0 {
		args = append(args, "--data-binary", "@-")
	}
	args = append(args, url)

	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, bastion.ShellQuote(arg))
	}

	var script strings.Builder
	script.WriteString("set -eu\n")
	if directory != "" {
		script.WriteString("cd ")
		script.WriteString(bastion.ShellQuote(directory))
		script.WriteString("\n")
	}

	if len(body) > 0 {
		script.WriteString("printf %s ")
		script.WriteString(bastion.ShellQuote(base64.StdEncoding.EncodeToString(body)))
		script.WriteString(" | base64 -d | ")
	}
	script.WriteString(strings.Join(quoted, " "))
	script.WriteString("\n")

	return script.String()
}

func responseText(contents []byte) string {
	var response struct {
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(contents, &response); err != nil {
		return ""
	}

	var parts []string
	for _, part := range response.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}

	return strings.Join(parts, "\n\n")
}

func attachmentMime(attachment linear.Attachment) string {
	for _, key := range []string{"contentType", "mimeType", "mime"} {
		if value, ok := attachment.Metadata[key].(string); ok && value != "" {
			return value
		}
	}

	if ext := filepath.Ext(attachment.URL); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return value
		}
	}

	return "application/octet-stream"
}

func attachmentFilename(attachment linear.Attachment) string {
	if attachment.Title != "" {
		return attachment.Title
	}
	if attachment.URL != "" {
		base := filepath.Base(attachment.URL)
		if base != "." && base != "/" {
			return base
		}
	}

	return attachment.ID
}
