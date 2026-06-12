//go:build darwin

// Package environment provides environment API types for the macOS client.
package environment

import "io"

// Environment describes a managed opencode environment.
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

// SSHConnection contains private connection metadata for API-managed SSH.
type SSHConnection struct {
	Host    string
	Port    int
	User    string
	KeyPath string
}

// Tunnel describes a template-registered environment tunnel.
type Tunnel struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// Tunnels contains the registered tunnels for one environment.
type Tunnels struct {
	Entries []Tunnel `json:"entries"`
}

// CreateRequest contains the fields needed to create an environment.
type CreateRequest struct {
	Key         *string   `json:"key,omitempty"`
	TemplateID  string    `json:"templateId,omitempty"`
	TemplateKey string    `json:"templateKey,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Logs        io.Writer `json:"-"`
}

// Stream event types used by POST /v1/environments.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// CreateStreamEvent is one line in a streamed environment creation response.
type CreateStreamEvent struct {
	Type        string       `json:"type"`
	Log         string       `json:"log,omitempty"`
	Environment *Environment `json:"environment,omitempty"`
	Error       string       `json:"error,omitempty"`
	Status      int          `json:"status,omitempty"`
}
