//go:build darwin

// Package template provides environment template API types for the macOS client.
package template

import (
	"encoding/json"
	"io"
)

// Template contains an environment template and its JSON configuration.
type Template struct {
	ID        string          `json:"id"`
	Key       *string         `json:"key,omitempty"`
	Config    json.RawMessage `json:"config"`
	CreatedAt string          `json:"createdAt"`
}

// Metadata describes a template without its full configuration payload.
type Metadata struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a template.
type CreateRequest struct {
	Key    *string         `json:"key,omitempty"`
	Config json.RawMessage `json:"config"`
	Logs   io.Writer       `json:"-"`
}

// Stream event types used by POST /v1/templates.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// CreateStreamEvent is one line in a streamed template creation response.
type CreateStreamEvent struct {
	Type     string    `json:"type"`
	Log      string    `json:"log,omitempty"`
	Template *Metadata `json:"template,omitempty"`
	Error    string    `json:"error,omitempty"`
	Status   int       `json:"status,omitempty"`
}
