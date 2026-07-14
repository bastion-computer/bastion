//nolint:wsl_v5 // Runtime orchestration keeps cleanup adjacent to each failure branch.
package cloudhypervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
)

const baseBuildDirName = ".base-build"

// Base resource errors.
var (
	ErrBaseExists         = errors.New("base already exists")
	ErrBaseNotFound       = errors.New("base not found")
	ErrInvalidBaseArchive = basearchive.ErrInvalid
)

// BuildBase boots a VM, installs template-agnostic runtime components, and stores it as the singleton base.
//
//nolint:gocyclo,funlen // Coordinates VM boot, networking, guest preparation, cleanup, and artifact install.
func (m Manager) BuildBase(ctx context.Context, req BuildBaseRequest) (basearchive.Metadata, error) {
	m = m.withDefaults()
	if err := writeLog(req.Logs, "preparing base workspace\n"); err != nil {
		return basearchive.Metadata{}, err
	}

	if !req.Force {
		if _, err := loadBase(m.DataDir); err == nil {
			return basearchive.Metadata{}, ErrBaseExists
		} else if !errors.Is(err, ErrBaseNotFound) {
			return basearchive.Metadata{}, err
		}
	}

	resources := resolvedResources{cpus: vmCPUs(), memoryBytes: vmMemoryBytes()}

	workspace, err := m.prepareBaseWorkspace(ctx, resources)
	if err != nil {
		return basearchive.Metadata{}, err
	}

	if err := writeLog(req.Logs, "booting base vm\n"); err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	baseVMID := "base"

	reservedVM, err := m.reserveNetwork(baseVMID, workspace.dir)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	plan, err := planNetwork(baseVMID, reservedVM.NetworkIndex)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	plan, err = m.setupTap(ctx, plan)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	dhcpPID, err := m.startDHCP(ctx, workspace.dir, plan)
	if err != nil {
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	if err := m.prepareCloudInit(ctx, baseVMID, workspace, plan); err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	vm, err := m.startMachine(ctx, baseVMID, workspace, plan, resources)
	if err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	vm.DHCPPID = dhcpPID

	if err := writeVMState(vm); err != nil {
		m.cleanupVM(context.Background(), vm, false)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	if err := waitForTCP(ctx, vm.GuestIP, vm.SSHPort, sshWait); err != nil {
		vm.State = StateError
		vm.LastError = err.Error()
		_ = writeVMState(vm)
		m.cleanupVM(context.Background(), vm, false)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	if err := m.waitForGuestSSH(ctx, vm, sshWait); err != nil {
		vm.State = StateError
		vm.LastError = err.Error()
		_ = writeVMState(vm)
		m.cleanupVM(context.Background(), vm, false)
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	metadata, err := m.prepareBaseArtifacts(ctx, vm, workspace, req.Logs)
	if err != nil {
		return basearchive.Metadata{}, err
	}

	m.cleanupVM(context.Background(), vm, false)
	_ = os.Remove(statePath(workspace.dir))

	if err := os.Chmod(workspace.rootfsPath, 0o400); err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, fmt.Errorf("mark base rootfs immutable: %w", err)
	}

	if err := writeLog(req.Logs, "installing base artifacts\n"); err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	metadata, err = m.installBase(ctx, workspace.dir, metadata)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return basearchive.Metadata{}, err
	}

	m.Logger.InfoContext(ctx, "prepared cloud-hypervisor base",
		slog.String("content_address", metadata.ContentAddress),
		slog.String("base_dir", baseDir(m.DataDir)),
	)

	return metadata, nil
}

// GetBase returns the current base metadata.
func (m Manager) GetBase(context.Context) (basearchive.Metadata, error) {
	m = m.withDefaults()

	return loadBase(m.DataDir)
}

// ExportBase streams a compressed archive containing base metadata and artifacts.
func (m Manager) ExportBase(ctx context.Context, req ExportBaseRequest) error {
	m = m.withDefaults()

	if req.Writer == nil {
		return errors.New("base archive writer is required")
	}

	metadata, err := loadBase(m.DataDir)
	if err != nil {
		return err
	}

	if err := basearchive.Write(ctx, req.Writer, metadata, basearchive.Files(baseDir(m.DataDir))); err != nil {
		return fmt.Errorf("write base archive: %w", err)
	}

	return nil
}

// ImportBase restores base artifacts from a compressed archive.
//
//nolint:gocyclo // Coordinates force checks, archive extraction, ownership fixes, logging, and atomic install.
func (m Manager) ImportBase(ctx context.Context, req ImportBaseRequest) (basearchive.Metadata, error) {
	m = m.withDefaults()

	if !req.Force {
		if _, err := loadBase(m.DataDir); err == nil {
			return basearchive.Metadata{}, ErrBaseExists
		} else if !errors.Is(err, ErrBaseNotFound) {
			return basearchive.Metadata{}, err
		}
	}

	if req.Reader == nil {
		return basearchive.Metadata{}, errors.New("base archive reader is required")
	}

	if err := writeLog(req.Logs, "importing base archive\n"); err != nil {
		return basearchive.Metadata{}, err
	}

	if err := os.MkdirAll(m.DataDir, 0o750); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("create data directory: %w", err)
	}

	tmpDir, err := os.MkdirTemp(m.DataDir, ".base-import-*")
	if err != nil {
		return basearchive.Metadata{}, fmt.Errorf("create import base directory: %w", err)
	}

	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	metadata, err := basearchive.Extract(ctx, req.Reader, tmpDir)
	if err != nil {
		return basearchive.Metadata{}, err
	}

	if err := chownIfConfigured(filepath.Join(tmpDir, basearchive.RootfsName), m.UID, m.GID); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("chown imported base rootfs: %w", err)
	}

	if metadata.CreatedAt == "" {
		metadata.CreatedAt = now()
	}
	metadata.UpdatedAt = now()

	if err := writeLog(req.Logs, "installing base archive\n"); err != nil {
		return basearchive.Metadata{}, err
	}

	metadata, err = m.installBase(ctx, tmpDir, metadata)
	if err != nil {
		return basearchive.Metadata{}, err
	}

	removeTemp = false

	return metadata, nil
}

func (m Manager) prepareBaseWorkspace(ctx context.Context, resources resolvedResources) (workspace, error) {
	assetSet, err := loadAssets(m.DataDir)
	if err != nil {
		return workspace{}, err
	}

	dir := filepath.Join(m.DataDir, baseBuildDirName)
	if err := os.RemoveAll(dir); err != nil {
		return workspace{}, fmt.Errorf("remove stale base build directory: %w", err)
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return workspace{}, fmt.Errorf("create base build directory: %w", err)
	}

	rootfsPath := filepath.Join(dir, envRootfsFileName)
	seedPath := filepath.Join(dir, envSeedFileName)
	snapshotPath := filepath.Join(dir, snapshotDirName)

	if err := copyFile(assetSet.rootfs, rootfsPath, 0o640); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if resources.rootfsSize != "" {
		if err := m.run(ctx, "qemu-img", "resize", rootfsPath, resources.rootfsSize); err != nil {
			_ = os.RemoveAll(dir)

			return workspace{}, err
		}
	}

	if err := chownIfConfigured(rootfsPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	return workspace{dir: dir, rootfsPath: rootfsPath, seedPath: seedPath, snapshotPath: snapshotPath, kernelPath: assetSet.kernel, initramfsPath: assetSet.initramfs, sshKeyPath: assetSet.sshKey, assets: assetSet}, nil
}

func (m Manager) prepareBaseArtifacts(ctx context.Context, vm VM, workspace workspace, logs io.Writer) (basearchive.Metadata, error) {
	if err := writeLog(logs, "installing base guest proxy\n"); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := m.setupTemplateGuestProxy(ctx, vm, logs); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := writeLog(logs, "installing base agents\n"); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := m.setupBaseAgents(ctx, vm, logs); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := writeLog(logs, "syncing base guest filesystem\n"); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := m.syncGuestFilesystem(ctx, vm); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	if err := copyFile(workspace.sshKeyPath, filepath.Join(workspace.dir, basearchive.SSHKeyName), 0o600); err != nil {
		return m.failBasePreparation(vm, workspace, err)
	}

	createdAt := now()

	return basearchive.Metadata{CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func (m Manager) failBasePreparation(vm VM, workspace workspace, err error) (basearchive.Metadata, error) {
	failed, failErr := failVM(vm, err)
	m.cleanupVM(context.Background(), failed, false)
	_ = os.RemoveAll(workspace.dir)

	return basearchive.Metadata{}, failErr
}

func (m Manager) installBase(ctx context.Context, srcDir string, metadata basearchive.Metadata) (basearchive.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return basearchive.Metadata{}, err
	}

	if err := m.ensureBaseSSHAccessAt(srcDir); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("prepare base SSH access: %w", err)
	}

	finalDir := baseDir(m.DataDir)
	if err := os.RemoveAll(finalDir); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("remove existing base: %w", err)
	}

	if err := os.Rename(srcDir, finalDir); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("install base artifacts: %w", err)
	}

	if metadata.CreatedAt == "" {
		metadata.CreatedAt = now()
	}
	if metadata.UpdatedAt == "" {
		metadata.UpdatedAt = metadata.CreatedAt
	}

	contentAddress, err := basearchive.ContentAddressForFiles(ctx, basearchive.Files(finalDir))
	if err != nil {
		_ = os.RemoveAll(finalDir)

		return basearchive.Metadata{}, err
	}

	metadata.ContentAddress = contentAddress

	if err := writeBaseMetadata(finalDir, metadata); err != nil {
		_ = os.RemoveAll(finalDir)

		return basearchive.Metadata{}, err
	}

	return metadata, nil
}

func (m Manager) ensureBaseSSHAccess() error {
	return m.ensureBaseSSHAccessAt(baseDir(m.DataDir))
}

func (m Manager) ensureBaseSSHAccessAt(dir string) error {
	dirInfo, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat base directory: %w", err)
	}
	if !dirInfo.IsDir() {
		return errors.New("base path is not a directory")
	}

	keyPath := filepath.Join(dir, basearchive.SSHKeyName)
	keyInfo, err := os.Lstat(keyPath)
	if err != nil {
		return fmt.Errorf("stat base SSH key: %w", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return errors.New("base SSH key is not a regular file")
	}

	if err := m.setProxyAccess(dir, 0o750); err != nil {
		return fmt.Errorf("prepare base directory access: %w", err)
	}
	if err := m.setProxyAccess(keyPath, 0o600); err != nil {
		return fmt.Errorf("prepare base SSH key access: %w", err)
	}

	return nil
}

func loadBase(dataDir string) (basearchive.Metadata, error) {
	dir := baseDir(dataDir)
	contents, err := os.ReadFile(filepath.Join(dir, baseMetadataFileName)) //nolint:gosec // Path is rooted in the configured Bastion data directory.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return basearchive.Metadata{}, ErrBaseNotFound
		}

		return basearchive.Metadata{}, fmt.Errorf("read base metadata: %w", err)
	}

	var metadata basearchive.Metadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return basearchive.Metadata{}, fmt.Errorf("parse base metadata: %w", err)
	}

	if strings.TrimSpace(metadata.ContentAddress) == "" {
		return basearchive.Metadata{}, errors.New("base metadata missing content address")
	}

	for _, file := range basearchive.Files(dir) {
		info, err := os.Stat(file.Path)
		if err != nil {
			return basearchive.Metadata{}, fmt.Errorf("base is missing %s: %w", file.Name, err)
		}

		if !info.Mode().IsRegular() {
			return basearchive.Metadata{}, fmt.Errorf("base file %s is not regular", file.Name)
		}
	}

	return metadata, nil
}

func writeBaseMetadata(dir string, metadata basearchive.Metadata) error {
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return atomicWriteFile(filepath.Join(dir, baseMetadataFileName), contents, 0o600)
}

func writeLog(logs io.Writer, message string) error {
	if logs == nil {
		return nil
	}

	_, err := io.WriteString(logs, message)

	return err
}
