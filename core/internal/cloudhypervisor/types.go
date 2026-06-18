// Package cloudhypervisor orchestrates Cloud Hypervisor runtime state.
package cloudhypervisor

import (
	"encoding/json"
	"io"
	"time"

	"github.com/bastion-computer/bastion/core/internal/tunnel"
)

const (
	// DefaultSocketPath is the Unix socket used by the privileged bastiond service.
	DefaultSocketPath = "/run/bastion/bastiond.sock"

	// SSHUser is the default guest user provisioned into the Bastion rootfs.
	SSHUser = "root"

	// SSHPort is the default SSH port exposed by the guest.
	SSHPort = 22

	// AgentOpenCode is the template agent key for OpenCode.
	AgentOpenCode = "opencode"

	// OpenCodeDefaultPort is the default OpenCode HTTP server port.
	OpenCodeDefaultPort = 4096

	// GuestProxyVsockPort is the fixed guest-side vsock port for HTTP tunnel proxying.
	GuestProxyVsockPort = tunnel.GuestProxyVsockPort
	// TemplateArchiveContentType is the media type used for streamed template backups.
	TemplateArchiveContentType = "application/vnd.bastion.template+tar+gzip"

	// NetworkIndexLimit is the number of /30 VM networks available in 10.241.0.0/16.
	NetworkIndexLimit = 16000

	// StateCreating means a VM is being launched.
	StateCreating = "creating"
	// StateRunning means a VM is live and reachable.
	StateRunning = "running"
	// StatePaused means Cloud Hypervisor reports the VM is paused.
	StatePaused = "paused"
	// StateStopped means no live VM is present.
	StateStopped = "stopped"
	// StateError means orchestration failed.
	StateError = "error"

	// StreamEventLog carries guest initialization command output.
	StreamEventLog = "log"
	// StreamEventResult carries the final successful stream payload.
	StreamEventResult = "result"
	// StreamEventError carries the final failed stream payload.
	StreamEventError = "error"
)

// Template describes the environment template supplied to VM orchestration.
// It is accepted now so the runtime API can evolve without changing callers.
type Template struct {
	ID     string          `json:"id"`
	Key    *string         `json:"key,omitempty"`
	Config json.RawMessage `json:"config"`
}

// LaunchRequest asks bastiond to launch a VM for an environment.
type LaunchRequest struct {
	EnvironmentID string    `json:"environmentId"`
	Template      Template  `json:"template"`
	Logs          io.Writer `json:"-"`
}

// PrepareTemplateRequest asks bastiond to prepare and snapshot a template VM.
type PrepareTemplateRequest struct {
	Template Template  `json:"template"`
	Logs     io.Writer `json:"-"`
}

// ExportTemplateRequest asks bastiond to stream prepared template artifacts.
type ExportTemplateRequest struct {
	Template Template  `json:"template"`
	Writer   io.Writer `json:"-"`
}

// ImportTemplateRequest asks bastiond to restore prepared template artifacts.
type ImportTemplateRequest struct {
	TemplateID string    `json:"templateId"`
	Reader     io.Reader `json:"-"`
}

// ImportedTemplate describes the template data found in an imported archive.
type ImportedTemplate struct {
	Template  Template `json:"template"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
}

// PreparedTemplate describes durable prepared template artifacts.
type PreparedTemplate struct {
	TemplateID  string `json:"templateId"`
	TemplateDir string `json:"templateDir,omitempty"`
	RootfsPath  string `json:"rootfsPath,omitempty"`
	SeedPath    string `json:"seedPath,omitempty"`
	SnapshotDir string `json:"snapshotDir,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// LaunchStreamEvent is one line in a streamed VM launch response.
type LaunchStreamEvent struct {
	Type   string `json:"type"`
	Log    string `json:"log,omitempty"`
	VM     *VM    `json:"vm,omitempty"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status,omitempty"`
}

// PrepareTemplateStreamEvent is one line in a streamed template preparation response.
type PrepareTemplateStreamEvent struct {
	Type     string            `json:"type"`
	Log      string            `json:"log,omitempty"`
	Template *PreparedTemplate `json:"template,omitempty"`
	Error    string            `json:"error,omitempty"`
	Status   int               `json:"status,omitempty"`
}

// VM describes durable VM runtime metadata.
type VM struct {
	EnvironmentID   string `json:"environmentId"`
	VMID            string `json:"vmId"`
	State           string `json:"state"`
	PID             int    `json:"pid,omitempty"`
	DHCPPID         int    `json:"dhcpPid,omitempty"`
	EnvDir          string `json:"envDir,omitempty"`
	RuntimeDir      string `json:"runtimeDir,omitempty"`
	SocketPath      string `json:"socketPath,omitempty"`
	VsockSocketPath string `json:"vsockSocketPath,omitempty"`
	KernelPath      string `json:"kernelPath,omitempty"`
	InitramfsPath   string `json:"initramfsPath,omitempty"`
	RootfsPath      string `json:"rootfsPath,omitempty"`
	TapName         string `json:"tapName,omitempty"`
	HostIP          string `json:"hostIp,omitempty"`
	GuestIP         string `json:"guestIp,omitempty"`
	GuestCIDR       string `json:"guestCidr,omitempty"`
	GuestMAC        string `json:"guestMac,omitempty"`
	NetworkIndex    int    `json:"networkIndex"`
	SSHUser         string `json:"sshUser,omitempty"`
	SSHPort         int    `json:"sshPort,omitempty"`
	SSHKeyPath      string `json:"sshKeyPath,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
	LastError       string `json:"lastError,omitempty"`
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
