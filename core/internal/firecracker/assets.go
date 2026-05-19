package firecracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	assetDirName      = "firecracker"
	environmentsDir   = "environments"
	manifestFileName  = "manifest.json"
	firecrackerName   = "firecracker"
	jailerName        = "jailer"
	envStateFileName  = "vm.json"
	envRootfsFileName = "rootfs.ext4"
	envKernelFileName = "kernel"
)

type manifest struct {
	Firecracker string `json:"firecracker"`
	Jailer      string `json:"jailer"`
	Kernel      string `json:"kernel"`
	RootFSExt4  string `json:"rootfs_ext4"`
	SSHKey      string `json:"ssh_key"`
}

type assets struct {
	firecracker string
	jailer      string
	kernel      string
	rootfs      string
	sshKey      string
}

func loadAssets(dataDir string) (assets, error) {
	assetDir := filepath.Join(dataDir, assetDirName)
	manifestPath := filepath.Join(assetDir, manifestFileName)

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Path is rooted in the configured Bastion data directory.
	if err != nil {
		return assets{}, fmt.Errorf("read firecracker manifest: %w", err)
	}

	var m manifest
	if err := json.Unmarshal(contents, &m); err != nil {
		return assets{}, fmt.Errorf("parse firecracker manifest: %w", err)
	}

	out := assets{
		firecracker: resolveAsset(assetDir, m.Firecracker),
		jailer:      resolveAsset(assetDir, m.Jailer),
		kernel:      resolveAsset(assetDir, m.Kernel),
		rootfs:      resolveAsset(assetDir, m.RootFSExt4),
		sshKey:      resolveAsset(assetDir, m.SSHKey),
	}

	if err := out.validate(); err != nil {
		return assets{}, err
	}

	return out, nil
}

func resolveAsset(assetDir, name string) string {
	if name == "" || filepath.IsAbs(name) {
		return name
	}

	return filepath.Join(assetDir, name)
}

func (a assets) validate() error {
	checks := []struct {
		name       string
		path       string
		executable bool
	}{
		{name: firecrackerName, path: a.firecracker, executable: true},
		{name: jailerName, path: a.jailer, executable: true},
		{name: "kernel", path: a.kernel},
		{name: "rootfs", path: a.rootfs},
		{name: "ssh key", path: a.sshKey},
	}

	for _, check := range checks {
		if check.path == "" {
			return fmt.Errorf("firecracker asset missing from manifest: %s", check.name)
		}

		info, err := os.Stat(check.path)
		if err != nil {
			return fmt.Errorf("stat firecracker asset %s: %w", check.name, err)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("firecracker asset %s is not a regular file", check.name)
		}

		if check.executable && info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("firecracker asset %s is not executable", check.name)
		}
	}

	return nil
}

func envDir(dataDir, environmentID string) string {
	return filepath.Join(dataDir, environmentsDir, environmentID)
}

func statePath(dir string) string {
	return filepath.Join(dir, envStateFileName)
}

func readVMState(dir string) (VM, error) {
	contents, err := os.ReadFile(statePath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return VM{}, fmt.Errorf("%w: vm state not found", os.ErrNotExist)
		}

		return VM{}, fmt.Errorf("read vm state: %w", err)
	}

	var vm VM
	if err := json.Unmarshal(contents, &vm); err != nil {
		return VM{}, fmt.Errorf("parse vm state: %w", err)
	}

	return vm, nil
}

func writeVMState(vm VM) error {
	vm.UpdatedAt = now()

	contents, err := json.MarshalIndent(vm, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return os.WriteFile(statePath(vm.EnvDir), contents, 0o600)
}
