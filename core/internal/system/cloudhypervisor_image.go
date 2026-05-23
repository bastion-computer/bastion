package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type cloudHypervisorImageBuilderImpl struct {
	runner Runner
	out    io.Writer
}

func (b cloudHypervisorImageBuilderImpl) build(
	ctx context.Context,
	store cloudHypervisorStore,
	manifest cloudHypervisorManifest,
) (cloudHypervisorManifest, error) {
	b = b.withDefaults()

	if manifest.RootFSImage == "" {
		return manifest, fmt.Errorf("cloud-hypervisor rootfs image is missing")
	}
	if manifest.Kernel == "" {
		return manifest, fmt.Errorf("cloud-hypervisor kernel is missing")
	}
	if manifest.Initramfs == "" {
		return manifest, fmt.Errorf("cloud-hypervisor initramfs is missing")
	}

	keyPath := filepath.Join(store.dir, "ubuntu-24.04.id_rsa")

	if err := logCloudHypervisorProgress(b.out, "generating SSH key"); err != nil {
		return manifest, err
	}

	if err := b.generateSSHKey(ctx, keyPath); err != nil {
		return manifest, err
	}

	manifest.SSHKey = filepath.Base(keyPath)

	return manifest, nil
}

func (b cloudHypervisorImageBuilderImpl) withDefaults() cloudHypervisorImageBuilderImpl {
	if b.runner == nil {
		b.runner = NewExecRunner(nil, nil)
	}

	return b
}

func (b cloudHypervisorImageBuilderImpl) generateSSHKey(ctx context.Context, keyPath string) error {
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH key: %w", err)
	}

	if err := os.Remove(keyPath + ".pub"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH public key: %w", err)
	}

	return b.runner.Run(ctx, utilitySSHKeygen, "-q", "-f", keyPath, "-N", "")
}
