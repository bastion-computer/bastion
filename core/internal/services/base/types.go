// Package base manages the template-agnostic base image resource.
package base

import (
	"io"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
)

// Base describes the current base image.
type Base = basearchive.Metadata

// BuildRequest contains the fields needed to build a base image.
type BuildRequest struct {
	Force bool      `json:"force,omitempty"`
	Logs  io.Writer `json:"-"`
}

// ImportRequest contains the fields needed to import a base image archive.
type ImportRequest struct {
	Force       bool      `json:"force,omitempty"`
	Archive     io.Reader `json:"-"`
	ArchiveSize int64     `json:"-"`
	Logs        io.Writer `json:"-"`
}

// ArchiveContentType is the media type used for base import/export streams.
const ArchiveContentType = basearchive.ContentType

// Stream event types used by base build and import routes.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// StreamEvent is one line in a streamed base build/import response.
type StreamEvent struct {
	Type   string `json:"type"`
	Log    string `json:"log,omitempty"`
	Base   *Base  `json:"base,omitempty"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status,omitempty"`
}
