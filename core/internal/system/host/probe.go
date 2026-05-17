// Package host probes generic host capabilities.
package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
)

const kvmPath = "/dev/kvm"

// Probe contains generic host probes used by system dependencies.
type Probe struct {
	DataDir string
	OS      string
	Arch    string

	LookPath       func(string) (string, error)
	Stat           func(string) (os.FileInfo, error)
	CheckKVMAccess func() error
}

// NewProbe returns a host probe for dataDir.
func NewProbe(dataDir string) Probe {
	return Probe{DataDir: dataDir}.WithDefaults()
}

// WithDefaults fills unset host probe hooks with real host implementations.
func (p Probe) WithDefaults() Probe {
	if p.OS == "" {
		p.OS = runtime.GOOS
	}

	if p.Arch == "" {
		p.Arch = firecrackerArch(runtime.GOARCH)
	}

	if p.LookPath == nil {
		p.LookPath = exec.LookPath
	}

	if p.Stat == nil {
		p.Stat = os.Stat
	}

	if p.CheckKVMAccess == nil {
		p.CheckKVMAccess = defaultKVMAccess
	}

	return p
}

// IsLinux reports whether the host OS is Linux.
func (p Probe) IsLinux() bool {
	return p.WithDefaults().OS == "linux"
}

// SupportsArch reports whether the host architecture matches any allowed architecture.
func (p Probe) SupportsArch(allowed ...string) bool {
	p = p.WithDefaults()

	return slices.Contains(allowed, p.Arch)
}

// KVMExists reports whether /dev/kvm exists.
func (p Probe) KVMExists() bool {
	_, err := p.WithDefaults().Stat(kvmPath)
	return err == nil
}

// KVMReadWrite reports whether /dev/kvm is readable and writable.
func (p Probe) KVMReadWrite() bool {
	return p.WithDefaults().CheckKVMAccess() == nil
}

// RegularFile reports whether path exists and is a regular file.
func (p Probe) RegularFile(path string) bool {
	if path == "" {
		return false
	}

	info, err := p.WithDefaults().Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// Executable reports whether path exists as an executable regular file.
func (p Probe) Executable(path string) bool {
	if !p.RegularFile(path) {
		return false
	}

	info, err := p.WithDefaults().Stat(path)

	return err == nil && info.Mode().Perm()&0o111 != 0
}

// KVMPath returns the Linux KVM device path.
func KVMPath() string {
	return kvmPath
}

func defaultKVMAccess() error {
	file, err := os.OpenFile(kvmPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}

	return file.Close()
}

func firecrackerArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

// JoinDataPath joins path elements relative to the probe data directory.
func (p Probe) JoinDataPath(elements ...string) string {
	items := append([]string{p.DataDir}, elements...)

	return filepath.Join(items...)
}
