package cloudhypervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	assetDirName        = "cloud-hypervisor"
	environmentsDir     = "environments"
	manifestFileName    = "manifest.json"
	cloudHypervisorName = "cloud-hypervisor"
	envStateFileName    = "vm.json"
	envRootfsFileName   = "rootfs.img"
	envSeedFileName     = "cidata.img"
)

type manifest struct {
	CloudHypervisor string `json:"cloud_hypervisor"`
	Kernel          string `json:"kernel"`
	Initramfs       string `json:"initramfs"`
	RootFSImage     string `json:"rootfs_image"`
	SSHKey          string `json:"ssh_key"`
}

type assets struct {
	cloudHypervisor string
	kernel          string
	initramfs       string
	rootfs          string
	sshKey          string
}

func loadAssets(dataDir string) (assets, error) {
	assetDir := filepath.Join(dataDir, assetDirName)
	manifestPath := filepath.Join(assetDir, manifestFileName)

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Path is rooted in the configured Bastion data directory.
	if err != nil {
		return assets{}, fmt.Errorf("read cloud-hypervisor manifest: %w", err)
	}

	var m manifest
	if err := json.Unmarshal(contents, &m); err != nil {
		return assets{}, fmt.Errorf("parse cloud-hypervisor manifest: %w", err)
	}

	out := assets{
		cloudHypervisor: resolveAsset(assetDir, m.CloudHypervisor),
		kernel:          resolveAsset(assetDir, m.Kernel),
		initramfs:       resolveAsset(assetDir, m.Initramfs),
		rootfs:          resolveAsset(assetDir, m.RootFSImage),
		sshKey:          resolveAsset(assetDir, m.SSHKey),
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
		{name: cloudHypervisorName, path: a.cloudHypervisor, executable: true},
		{name: "kernel", path: a.kernel},
		{name: "initramfs", path: a.initramfs},
		{name: "rootfs", path: a.rootfs},
		{name: "ssh key", path: a.sshKey},
	}

	for _, check := range checks {
		if check.path == "" {
			return fmt.Errorf("cloud-hypervisor asset missing from manifest: %s", check.name)
		}

		info, err := os.Stat(check.path)
		if err != nil {
			return fmt.Errorf("stat cloud-hypervisor asset %s: %w", check.name, err)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("cloud-hypervisor asset %s is not a regular file", check.name)
		}

		if check.executable && info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("cloud-hypervisor asset %s is not executable", check.name)
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
