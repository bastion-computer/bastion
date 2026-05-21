// Package firecracker orchestrates Firecracker microVM runtime state.
package firecracker

import (
	"encoding/json"
	"time"
)

const (
	// DefaultSocketPath is the Unix socket used by the privileged bastiond service.
	DefaultSocketPath = "/run/bastion/bastiond.sock"

	// SSHUser is the default guest user provisioned into the Bastion rootfs.
	SSHUser = "root"

	// SSHPort is the default SSH port exposed by the guest.
	SSHPort = 22
	// NetworkIndexLimit is the number of /30 VM networks available in 10.241.0.0/16.
	NetworkIndexLimit = 16000

	// StateCreating means a VM is being launched.
	StateCreating = "creating"
	// StateRunning means a VM is live and reachable.
	StateRunning = "running"
	// StatePaused means Firecracker reports the VM is paused.
	StatePaused = "paused"
	// StateStopped means no live VM is present.
	StateStopped = "stopped"
	// StateError means orchestration failed.
	StateError = "error"
)

// Template describes the environment template supplied to VM orchestration.
// It is accepted now so the runtime API can evolve without changing callers.
type Template struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Config json.RawMessage `json:"config"`
}

// LaunchRequest asks bastiond to launch a Firecracker VM for an environment.
type LaunchRequest struct {
	EnvironmentID string   `json:"environmentId"`
	Template      Template `json:"template"`
}

// VM describes durable VM runtime metadata.
type VM struct {
	EnvironmentID string `json:"environmentId"`
	VMID          string `json:"vmId"`
	State         string `json:"state"`
	PID           int    `json:"pid,omitempty"`
	EnvDir        string `json:"envDir,omitempty"`
	JailerDir     string `json:"jailerDir,omitempty"`
	SocketPath    string `json:"socketPath,omitempty"`
	KernelPath    string `json:"kernelPath,omitempty"`
	RootfsPath    string `json:"rootfsPath,omitempty"`
	TapName       string `json:"tapName,omitempty"`
	HostIP        string `json:"hostIp,omitempty"`
	GuestIP       string `json:"guestIp,omitempty"`
	GuestCIDR     string `json:"guestCidr,omitempty"`
	GuestMAC      string `json:"guestMac,omitempty"`
	NetworkIndex  int    `json:"networkIndex"`
	SSHUser       string `json:"sshUser,omitempty"`
	SSHPort       int    `json:"sshPort,omitempty"`
	SSHKeyPath    string `json:"sshKeyPath,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
