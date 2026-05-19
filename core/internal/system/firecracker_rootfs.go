package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultRootfsSize int64 = 1 << 30

type firecrackerExt4Builder struct {
	runner Runner
	out    io.Writer
	size   int64
}

func (b firecrackerExt4Builder) build(
	ctx context.Context,
	store firecrackerStore,
	manifest firecrackerManifest,
) (firecrackerManifest, error) {
	b = b.withDefaults()

	if manifest.RootFSSquashfs == "" {
		return manifest, errors.New("firecracker rootfs squashfs is missing")
	}

	rootfsName := strings.TrimSuffix(filepath.Base(manifest.RootFSSquashfs), ".squashfs")
	workDir := filepath.Join(store.dir, "rootfs-work")
	squashfsPath := filepath.Join(store.dir, manifest.RootFSSquashfs)
	keyPath := filepath.Join(store.dir, rootfsName+".id_rsa")
	ext4Path := filepath.Join(store.dir, rootfsName+".ext4")

	if err := prepareRootfsWorkDir(workDir); err != nil {
		return manifest, err
	}

	if err := logFirecrackerProgress(b.out, "extracting squashfs rootfs"); err != nil {
		return manifest, err
	}

	if err := b.runner.Run(ctx, utilityUnsquashfs, "-d", workDir, squashfsPath); err != nil {
		return manifest, err
	}

	if err := logFirecrackerProgress(b.out, "generating SSH key"); err != nil {
		return manifest, err
	}

	if err := b.generateSSHKey(ctx, keyPath); err != nil {
		return manifest, err
	}

	if err := logFirecrackerProgress(b.out, "adding SSH key to rootfs"); err != nil {
		return manifest, err
	}

	if err := authorizeSSHKey(workDir, keyPath+".pub"); err != nil {
		return manifest, err
	}

	if err := configureRootfsDNS(workDir); err != nil {
		return manifest, err
	}

	if err := logFirecrackerProgress(b.out, "setting rootfs ownership"); err != nil {
		return manifest, err
	}

	if err := b.runner.Run(ctx, utilitySudo, utilityChown, "-R", "root:root", workDir); err != nil {
		return manifest, err
	}

	defer func() {
		owner := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
		_ = b.runner.Run(ctx, utilitySudo, utilityChown, "-R", owner, workDir)
		_ = os.RemoveAll(workDir)
	}()

	if err := b.createExt4(ctx, workDir, ext4Path); err != nil {
		return manifest, err
	}

	manifest.SSHKey = filepath.Base(keyPath)
	manifest.RootFSExt4 = filepath.Base(ext4Path)

	return manifest, nil
}

func (b firecrackerExt4Builder) withDefaults() firecrackerExt4Builder {
	if b.runner == nil {
		b.runner = NewExecRunner(nil, nil)
	}

	if b.size == 0 {
		b.size = defaultRootfsSize
	}

	return b
}

func prepareRootfsWorkDir(workDir string) error {
	if err := os.RemoveAll(workDir); err != nil {
		return fmt.Errorf("remove rootfs work directory: %w", err)
	}

	return nil
}

func (b firecrackerExt4Builder) generateSSHKey(ctx context.Context, keyPath string) error {
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH key: %w", err)
	}

	if err := os.Remove(keyPath + ".pub"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing SSH public key: %w", err)
	}

	return b.runner.Run(ctx, utilitySSHKeygen, "-q", "-f", keyPath, "-N", "")
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

// configureRootfsDNS points glibc resolver config at the kernel autoconfig nameservers.
func configureRootfsDNS(workDir string) error {
	etcDir := filepath.Join(workDir, "etc")
	//nolint:gosec // /etc inside the guest rootfs must stay world-readable.
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		return fmt.Errorf("create rootfs etc directory: %w", err)
	}

	resolvConf := filepath.Join(etcDir, "resolv.conf")
	if err := os.Remove(resolvConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove rootfs resolv.conf: %w", err)
	}

	if err := os.Symlink("/proc/net/pnp", resolvConf); err != nil {
		return fmt.Errorf("link rootfs resolv.conf: %w", err)
	}

	return nil
}

// createExt4 creates and validates a Firecracker guest ext4 image.
//
//nolint:gosec // The rootfs path is constrained to the Firecracker data directory.
func (b firecrackerExt4Builder) createExt4(ctx context.Context, workDir, ext4Path string) error {
	if err := logFirecrackerProgress(b.out, "creating ext4 rootfs image"); err != nil {
		return err
	}

	file, err := os.OpenFile(ext4Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create ext4 rootfs: %w", err)
	}

	if err := file.Truncate(b.size); err != nil {
		_ = file.Close()

		return fmt.Errorf("size ext4 rootfs: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close ext4 rootfs: %w", err)
	}

	if err := b.runner.Run(ctx, utilitySudo, utilityMkfsExt4, "-d", workDir, "-F", ext4Path); err != nil {
		return err
	}

	if err := logFirecrackerProgress(b.out, "validating ext4 rootfs image"); err != nil {
		return err
	}

	return b.runner.Run(ctx, utilityE2fsck, "-fn", ext4Path)
}
