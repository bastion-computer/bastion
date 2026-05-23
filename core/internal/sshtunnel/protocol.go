// Package sshtunnel defines the wire protocol used by bastion ssh.
package sshtunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const (
	// Protocol is the HTTP Upgrade token for Bastion SSH sessions.
	Protocol = "bastion-ssh"

	// FrameStdin carries bytes from the CLI to the remote SSH session.
	FrameStdin byte = 1
	// FrameStdinEOF tells the API server that CLI stdin reached EOF.
	FrameStdinEOF byte = 2
	// FrameResize carries a terminal window size update.
	FrameResize byte = 3
	// FrameStdout carries remote stdout bytes.
	FrameStdout byte = 4
	// FrameStderr carries remote stderr bytes.
	FrameStderr byte = 5
	// FrameExit carries the remote command exit status.
	FrameExit byte = 6
	// FrameError carries an API-side SSH error.
	FrameError byte = 7

	// MaxPayload keeps malformed streams from allocating unbounded memory.
	MaxPayload = 1 << 20
)

// Request configures an API-managed SSH session.
type Request struct {
	Command []string `json:"command,omitempty"`
	PTY     bool     `json:"pty,omitempty"`
	Term    string   `json:"term,omitempty"`
	Width   int      `json:"width,omitempty"`
	Height  int      `json:"height,omitempty"`
}

// Resize describes a terminal window size.
type Resize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ExitStatus describes the remote command exit state.
type ExitStatus struct {
	Code int `json:"code"`
}

// FrameWriter serializes concurrent frame writes.
type FrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewFrameWriter returns a synchronized frame writer.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w}
}

// WriteFrame writes one frame while holding the writer lock.
func (w *FrameWriter) WriteFrame(frameType byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return WriteFrame(w.w, frameType, payload)
}

// WriteFrame writes one protocol frame.
func WriteFrame(w io.Writer, frameType byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("ssh tunnel frame payload too large: %d", len(payload))
	}

	payloadLen := uint32(len(payload)) //nolint:gosec // len(payload) is bounded by MaxPayload before conversion.
	header := [5]byte{frameType}
	binary.BigEndian.PutUint32(header[1:], payloadLen)

	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	if len(payload) == 0 {
		return nil
	}

	_, err := w.Write(payload)

	return err
}

// ReadFrame reads one protocol frame.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}

	length := binary.BigEndian.Uint32(header[1:])
	if length > MaxPayload {
		return 0, nil, fmt.Errorf("ssh tunnel frame payload too large: %d", length)
	}

	payload := make([]byte, int(length))
	if length == 0 {
		return header[0], payload, nil
	}

	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}

	return header[0], payload, nil
}
