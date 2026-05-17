package firecracker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/system/command"
)

const defaultRootfsSize = 1 << 30

// RootFSBuilder converts a downloaded squashfs rootfs into an ext4 rootfs.
type RootFSBuilder struct {
	Runner command.Runner
	Size   int64
}

// Build converts the downloaded Firecracker squashfs into an ext4 rootfs.
func (b RootFSBuilder) Build(ctx context.Context, store Store, manifest Manifest) (Manifest, error) {
	b = b.withDefaults()

	if manifest.RootFSSquashfs == "" {
		return manifest, errors.New("firecracker rootfs squashfs is missing")
	}

	rootfsName := strings.TrimSuffix(filepath.Base(manifest.RootFSSquashfs), ".squashfs")
	workDir := filepath.Join(store.Dir, "rootfs-work")
	squashfsPath := filepath.Join(store.Dir, manifest.RootFSSquashfs)
	keyPath := filepath.Join(store.Dir, rootfsName+".id_rsa")
	ext4Path := filepath.Join(store.Dir, rootfsName+".ext4")

	if err := b.prepareWorkDir(workDir); err != nil {
		return manifest, err
	}

	if err := b.Runner.Run(ctx, utilityUnsquashfs, "-d", workDir, squashfsPath); err != nil {
		return manifest, err
	}

	if err := b.generateSSHKey(ctx, keyPath); err != nil {
		return manifest, err
	}

	if err := authorizeSSHKey(workDir, keyPath+".pub"); err != nil {
		return manifest, err
	}

	if err := b.Runner.Run(ctx, utilitySudo, utilityChown, "-R", "root:root", workDir); err != nil {
		return manifest, err
	}

	defer func() {
		_ = b.Runner.Run(ctx, utilitySudo, utilityChown, "-R", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), workDir)
		_ = os.RemoveAll(workDir)
	}()

	if err := b.createExt4(ctx, workDir, ext4Path); err != nil {
		return manifest, err
	}

	manifest.SSHKey = filepath.Base(keyPath)
	manifest.RootFSExt4 = filepath.Base(ext4Path)

	return manifest, nil
}

func (b RootFSBuilder) withDefaults() RootFSBuilder {
	if b.Runner == nil {
		b.Runner = command.ExecRunner{}
	}

	if b.Size == 0 {
		b.Size = defaultRootfsSize
	}

	return b
}

func (b RootFSBuilder) prepareWorkDir(workDir string) error {
	if err := os.RemoveAll(workDir); err != nil {
		return fmt.Errorf("remove rootfs work directory: %w", err)
	}

	if err := os.MkdirAll(workDir, 0o750); err != nil {
		return fmt.Errorf("create rootfs work directory: %w", err)
	}

	return nil
}

func (b RootFSBuilder) generateSSHKey(ctx context.Context, keyPath string) error {
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH key: %w", err)
	}

	if err := os.Remove(keyPath + ".pub"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH public key: %w", err)
	}

	return b.Runner.Run(ctx, utilitySSHKeygen, "-q", "-f", keyPath, "-N", "")
}

// authorizeSSHKey writes the generated public key into the extracted rootfs.
//
//nolint:gosec // Paths are constrained to the generated rootfs work directory.
func authorizeSSHKey(workDir, publicKeyPath string) error {
	publicKey, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("read SSH public key: %w", err)
	}

	sshDir := filepath.Join(workDir, "root", ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create root ssh directory: %w", err)
	}

	authorizedKeys := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(authorizedKeys, publicKey, 0o600); err != nil {
		return fmt.Errorf("write authorized keys: %w", err)
	}

	return nil
}

// createExt4 creates and validates a Firecracker guest ext4 image.
//
//nolint:gosec // The rootfs path is constrained to the Firecracker data directory.
func (b RootFSBuilder) createExt4(ctx context.Context, workDir, ext4Path string) error {
	file, err := os.OpenFile(ext4Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create ext4 rootfs: %w", err)
	}

	if err := file.Truncate(b.Size); err != nil {
		_ = file.Close()
		return fmt.Errorf("size ext4 rootfs: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close ext4 rootfs: %w", err)
	}

	if err := b.Runner.Run(ctx, utilityMkfsExt4, "-d", workDir, "-F", ext4Path); err != nil {
		return err
	}

	return b.Runner.Run(ctx, utilityE2fsck, "-fn", ext4Path)
}
