package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
)

const (
	apiSocketName        = "api.socket"
	vsockSocketName      = "vsock.socket"
	proxyDirMode         = 0o750
	proxySocketMode      = 0o660
	runtimeDir           = "/run/bastion/vms"
	sshWait              = 180 * time.Second
	sshAuthRetryInterval = time.Second
	guestReadyCommand    = "if command -v cloud-init >/dev/null 2>&1; then timeout 120s cloud-init status --wait >/dev/null 2>&1 || true; fi"
	apiWait              = 15 * time.Second
	vmmStartErrorTimeout = 5 * time.Second
	snapshotNetworkDelay = 1500 * time.Millisecond
	vmLivenessPoll       = 100 * time.Millisecond
	vmCPUsEnv            = "BASTION_VM_CPUS"
	vmMemoryBytesEnv     = "BASTION_VM_MEMORY_BYTES"
	linuxCmdline         = "root=LABEL=cloudimg-rootfs rootwait ro console=ttyS0"
	defaultCPUs          = 2
	defaultMemoryBytes   = 2 << 30
	defaultRootfsSize    = "20G"
	gibBytes             = int64(1 << 30)
	defaultRootfsBytes   = 20 * gibBytes
)

// Manager performs privileged Cloud Hypervisor operations for bastiond.
type Manager struct {
	DataDir        string
	UID            int
	GID            int
	ProxyUID       int
	ProxyGID       int
	GuestProxyPath string
	Logger         *slog.Logger
	run            func(context.Context, string, ...string) error
	stream         func(context.Context, io.Writer, string, ...string) error
	output         func(context.Context, string, ...string) (string, error)
}

// NewManager returns a Cloud Hypervisor VM manager.
func NewManager(dataDir string, uid, gid int, logger *slog.Logger) Manager {
	return Manager{
		DataDir:  dataDir,
		UID:      uid,
		GID:      gid,
		ProxyUID: uid,
		ProxyGID: gid,
		Logger:   logger,
		run:      runCommand,
		stream:   runCommandStream,
		output:   outputCommand,
	}
}

// Launch creates and starts a Cloud Hypervisor VM.
func (m Manager) Launch(ctx context.Context, req LaunchRequest) (VM, error) {
	m = m.withDefaults()

	if strings.TrimSpace(req.EnvironmentID) == "" {
		return VM{}, errors.New("environment id is required")
	}

	resources, err := resolveTemplateResources(req.Template.Config)
	if err != nil {
		return VM{}, err
	}

	workspace, err := m.prepareRestoreWorkspace(ctx, req.EnvironmentID, req.Template)
	if err != nil {
		return VM{}, err
	}

	reservedVM, err := m.reserveNetwork(req.EnvironmentID, workspace.dir)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	plan, err := planNetwork(req.EnvironmentID, reservedVM.NetworkIndex)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	plan, err = m.setupTap(ctx, plan)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	dhcpPID, err := m.startDHCP(ctx, workspace.dir, plan)
	if err != nil {
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	if err := m.prepareCloudInit(ctx, req.EnvironmentID, workspace, plan); err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	vm, err := m.startMachine(ctx, req.EnvironmentID, workspace, plan, resources)
	if err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	vm.DHCPPID = dhcpPID

	if err := writeVMState(vm); err != nil {
		m.cleanupVM(context.Background(), vm, true)

		return VM{}, err
	}

	return m.completeLaunch(ctx, vm, req)
}

func (m Manager) completeLaunch(ctx context.Context, vm VM, req LaunchRequest) (VM, error) {
	if err := waitForTCP(ctx, vm.GuestIP, vm.SSHPort, sshWait); err != nil {
		return m.failLaunchReadiness(vm, err)
	}

	if err := m.waitForGuestSSH(ctx, vm, sshWait); err != nil {
		return m.failLaunchReadiness(vm, err)
	}

	if err := m.startEnvironmentServices(ctx, vm, req); err != nil {
		failed, failErr := failVM(vm, err)
		m.cleanupVM(context.Background(), failed, false)

		return failed, failErr
	}

	vm.State = StateRunning
	if err := writeVMState(vm); err != nil {
		m.cleanupVM(context.Background(), vm, true)

		return VM{}, err
	}

	m.Logger.InfoContext(ctx, "launched cloud-hypervisor vm",
		slog.String("environment_id", vm.EnvironmentID),
		slog.String("vm_id", vm.VMID),
		slog.Int("pid", vm.PID),
		slog.String("guest_ip", vm.GuestIP),
		slog.String("tap", vm.TapName),
	)

	return vm, nil
}

func (m Manager) failLaunchReadiness(vm VM, err error) (VM, error) {
	vm.State = StateError
	vm.LastError = err.Error()
	_ = writeVMState(vm)
	m.cleanupVM(context.Background(), vm, true)

	return VM{}, err
}

func (m Manager) startEnvironmentServices(ctx context.Context, vm VM, req LaunchRequest) error {
	if err := m.startEnvironmentGuestProxy(ctx, vm, req.Logs); err != nil {
		return err
	}

	if err := m.startEnvironmentAgents(ctx, vm, req.Template.Config, req.Logs); err != nil {
		return err
	}

	return m.runStartActions(ctx, vm, req.Template.Config, req.Logs)
}

func (m Manager) waitForGuestSSH(ctx context.Context, vm VM, timeout time.Duration) error {
	return m.waitForGuestSSHWithInterval(ctx, vm, timeout, sshAuthRetryInterval)
}

func (m Manager) waitForGuestSSHWithInterval(ctx context.Context, vm VM, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)

	port := vm.SSHPort
	if port == 0 {
		port = SSHPort
	}

	address := net.JoinHostPort(vm.GuestIP, strconv.Itoa(port))

	var lastErr error

	for {
		if err := m.runGuestCommand(ctx, vm, "true", nil); err != nil {
			lastErr = err
		} else if err := m.runGuestCommand(ctx, vm, guestReadyCommand, nil); err != nil {
			lastErr = err
		} else {
			return nil
		}

		if !time.Now().Before(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	if lastErr != nil {
		return fmt.Errorf("timed out waiting for SSH authentication on %s: %w", address, lastErr)
	}

	return fmt.Errorf("timed out waiting for SSH authentication on %s", address)
}

// PrepareTemplate boots a template VM, runs init actions, and stores reusable artifacts.
func (m Manager) PrepareTemplate(ctx context.Context, req PrepareTemplateRequest) (PreparedTemplate, error) {
	m = m.withDefaults()

	resources, workspace, baseMetadata, err := m.prepareTemplateInputs(ctx, req)
	if err != nil {
		return PreparedTemplate{}, err
	}

	templateVMID := templateNetworkID(req.Template.ID)

	reservedVM, err := m.reserveNetwork(templateVMID, workspace.dir)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	plan, err := planNetwork(templateVMID, reservedVM.NetworkIndex)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	plan, err = m.setupTap(ctx, plan)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	dhcpPID, err := m.startDHCP(ctx, workspace.dir, plan)
	if err != nil {
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	if err := m.prepareCloudInit(ctx, templateVMID, workspace, plan); err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	vm, err := m.startMachine(ctx, templateVMID, workspace, plan, resources)
	if err != nil {
		_ = terminateProcess(dhcpPID, vmmStartErrorTimeout)
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	vm.DHCPPID = dhcpPID

	if err := writeVMState(vm); err != nil {
		m.cleanupVM(context.Background(), vm, false)

		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	if err := waitForTCP(ctx, vm.GuestIP, vm.SSHPort, sshWait); err != nil {
		vm.State = StateError
		vm.LastError = err.Error()
		_ = writeVMState(vm)
		m.cleanupVM(context.Background(), vm, false)

		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	if err := m.waitForGuestSSH(ctx, vm, sshWait); err != nil {
		vm.State = StateError
		vm.LastError = err.Error()
		_ = writeVMState(vm)
		m.cleanupVM(context.Background(), vm, false)

		_ = os.RemoveAll(workspace.dir)

		return PreparedTemplate{}, err
	}

	prepared, err := m.prepareTemplateArtifacts(ctx, req, vm, workspace, baseMetadata.ContentAddress)
	if err != nil {
		return PreparedTemplate{}, err
	}

	m.cleanupVM(context.Background(), vm, false)

	_ = os.Remove(statePath(workspace.dir))

	m.Logger.InfoContext(ctx, "prepared cloud-hypervisor template",
		slog.String("template_id", req.Template.ID),
		slog.String("template_dir", prepared.TemplateDir),
	)

	return prepared, nil
}

func (m Manager) prepareTemplateArtifacts(ctx context.Context, req PrepareTemplateRequest, vm VM, workspace workspace, baseContentAddress string) (PreparedTemplate, error) {
	if err := m.setupTemplateAgents(ctx, vm, req.Template.Config, req.Logs); err != nil {
		return m.failTemplatePreparation(vm, workspace, err)
	}

	if err := m.runInitActions(ctx, vm, req.Template.Config, req.Logs); err != nil {
		return m.failTemplatePreparation(vm, workspace, err)
	}

	if err := m.prepareGuestForTemplateClone(ctx, vm); err != nil {
		return m.failTemplatePreparation(vm, workspace, err)
	}

	prepared, err := m.finishTemplateArtifacts(req.Template.ID, workspace, baseContentAddress)
	if err != nil {
		return m.failTemplatePreparation(vm, workspace, err)
	}

	return prepared, nil
}

func (m Manager) failTemplatePreparation(vm VM, workspace workspace, err error) (PreparedTemplate, error) {
	failed, failErr := failVM(vm, err)
	m.cleanupVM(context.Background(), failed, false)

	_ = os.RemoveAll(workspace.dir)

	return PreparedTemplate{}, failErr
}

func (m Manager) prepareTemplateInputs(ctx context.Context, req PrepareTemplateRequest) (resolvedResources, workspace, basearchive.Metadata, error) {
	if strings.TrimSpace(req.Template.ID) == "" {
		return resolvedResources{}, workspace{}, basearchive.Metadata{}, errors.New("template id is required")
	}

	resources, err := resolveTemplateResources(req.Template.Config)
	if err != nil {
		return resolvedResources{}, workspace{}, basearchive.Metadata{}, err
	}

	baseMetadata, err := loadBase(m.DataDir)
	if err != nil {
		return resolvedResources{}, workspace{}, basearchive.Metadata{}, err
	}

	ws, err := m.prepareTemplateWorkspace(ctx, req.Template.ID, resources, baseMetadata)
	if err != nil {
		return resolvedResources{}, workspace{}, basearchive.Metadata{}, err
	}

	return resources, ws, baseMetadata, nil
}

// RemoveTemplate removes prepared template artifacts.
func (m Manager) RemoveTemplate(_ context.Context, templateID string) (PreparedTemplate, error) {
	m = m.withDefaults()

	if strings.TrimSpace(templateID) == "" {
		return PreparedTemplate{}, errors.New("template id is required")
	}

	dir := templateDir(m.DataDir, templateID)
	prepared := PreparedTemplate{TemplateID: templateID, TemplateDir: dir, RootfsPath: filepath.Join(dir, envRootfsFileName), SeedPath: filepath.Join(dir, envSeedFileName), SnapshotDir: filepath.Join(dir, snapshotDirName), UpdatedAt: now()}

	if err := os.RemoveAll(dir); err != nil {
		return PreparedTemplate{}, fmt.Errorf("remove prepared template artifacts: %w", err)
	}

	return prepared, nil
}

type workspace struct {
	dir                string
	rootfsPath         string
	rootfsBackingFiles bool
	seedPath           string
	snapshotPath       string
	kernelPath         string
	initramfsPath      string
	sshKeyPath         string
	assets             assets
}

func (m Manager) prepareTemplateWorkspace(ctx context.Context, templateID string, resources resolvedResources, baseMetadata basearchive.Metadata) (workspace, error) {
	assetSet, err := loadAssets(m.DataDir)
	if err != nil {
		return workspace{}, err
	}

	if strings.TrimSpace(baseMetadata.ContentAddress) == "" {
		return workspace{}, errors.New("base content address is required")
	}

	dir := templateDir(m.DataDir, templateID)
	if err := os.RemoveAll(dir); err != nil {
		return workspace{}, fmt.Errorf("remove stale template directory: %w", err)
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return workspace{}, fmt.Errorf("create template directory: %w", err)
	}

	rootfsPath := filepath.Join(dir, envRootfsFileName)
	seedPath := filepath.Join(dir, envSeedFileName)
	snapshotPath := filepath.Join(dir, snapshotDirName)
	baseRootfsPath := filepath.Join(baseDir(m.DataDir), basearchive.RootfsName)
	baseSSHKeyPath := filepath.Join(baseDir(m.DataDir), basearchive.SSHKeyName)

	if err := m.run(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", baseRootfsPath, rootfsPath, resources.rootfsSize); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := chownIfConfigured(rootfsPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	return workspace{dir: dir, rootfsPath: rootfsPath, rootfsBackingFiles: true, seedPath: seedPath, snapshotPath: snapshotPath, kernelPath: assetSet.kernel, initramfsPath: assetSet.initramfs, sshKeyPath: baseSSHKeyPath, assets: assetSet}, nil
}

func (m Manager) prepareRestoreWorkspace(ctx context.Context, environmentID string, template Template) (workspace, error) {
	assetSet, err := loadAssets(m.DataDir)
	if err != nil {
		return workspace{}, err
	}

	prepared, err := loadPreparedTemplate(m.DataDir, template.ID)
	if err != nil {
		return workspace{}, err
	}

	if strings.TrimSpace(template.BaseContentAddress) == "" {
		return workspace{}, errors.New("template base content address is required")
	}

	if prepared.BaseContentAddress != template.BaseContentAddress {
		return workspace{}, fmt.Errorf("prepared template base content address %s does not match template metadata %s", prepared.BaseContentAddress, template.BaseContentAddress)
	}

	baseMetadata, err := loadBase(m.DataDir)
	if err != nil {
		return workspace{}, err
	}

	if baseMetadata.ContentAddress != template.BaseContentAddress {
		return workspace{}, fmt.Errorf("base content address %s does not match template base %s", baseMetadata.ContentAddress, template.BaseContentAddress)
	}

	dir := envDir(m.DataDir, environmentID)
	if err := os.RemoveAll(dir); err != nil {
		return workspace{}, fmt.Errorf("remove stale environment directory: %w", err)
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return workspace{}, fmt.Errorf("create environment directory: %w", err)
	}

	rootfsPath := filepath.Join(dir, envRootfsFileName)
	if err := m.run(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", prepared.RootfsPath, rootfsPath); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := chownIfConfigured(rootfsPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	snapshotPath := filepath.Join(dir, snapshotDirName)
	sshKeyPath := prepared.SSHKeyPath

	if sshKeyPath == "" {
		sshKeyPath = filepath.Join(baseDir(m.DataDir), basearchive.SSHKeyName)
	}

	return workspace{dir: dir, rootfsPath: rootfsPath, rootfsBackingFiles: true, seedPath: filepath.Join(dir, envSeedFileName), snapshotPath: snapshotPath, kernelPath: assetSet.kernel, initramfsPath: assetSet.initramfs, sshKeyPath: sshKeyPath, assets: assetSet}, nil
}

func loadPreparedTemplate(dataDir, templateID string) (PreparedTemplate, error) {
	if strings.TrimSpace(templateID) == "" {
		return PreparedTemplate{}, errors.New("template id is required")
	}

	dir := templateDir(dataDir, templateID)
	prepared := PreparedTemplate{
		TemplateID:  templateID,
		TemplateDir: dir,
		RootfsPath:  filepath.Join(dir, envRootfsFileName),
		SeedPath:    filepath.Join(dir, envSeedFileName),
		SnapshotDir: filepath.Join(dir, snapshotDirName),
		SSHKeyPath:  preparedTemplateSSHKeyPath(dir),
	}

	metadata, err := readPreparedTemplateMetadata(dir)
	if err != nil {
		return PreparedTemplate{}, err
	}

	prepared.BaseContentAddress = metadata.BaseContentAddress

	checks := []string{
		prepared.RootfsPath,
	}
	if prepared.SSHKeyPath != "" {
		checks = append(checks, prepared.SSHKeyPath, prepared.SSHKeyPath+".pub")
	}

	for _, path := range checks {
		info, err := os.Stat(path)
		if err != nil {
			return PreparedTemplate{}, fmt.Errorf("prepared template %s is missing %s: %w", templateID, filepath.Base(path), err)
		}

		if !info.Mode().IsRegular() {
			return PreparedTemplate{}, fmt.Errorf("prepared template %s file %s is not regular", templateID, filepath.Base(path))
		}
	}

	return prepared, nil
}

// State reconciles durable VM state with the running Cloud Hypervisor process.
func (m Manager) State(ctx context.Context, environmentID string) (VM, error) {
	m = m.withDefaults()
	dir := envDir(m.DataDir, environmentID)

	vm, err := readVMState(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
		}

		return VM{}, err
	}

	if vm.State == StateError {
		return vm, nil
	}

	if vm.PID == 0 || !processExists(vm.PID) || vm.SocketPath == "" {
		m.cleanupStoppedVM(ctx, vm)

		return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
	}

	info, clientErr := cloudHypervisorVMInfo(ctx, vm.SocketPath)
	if clientErr == nil {
		vm.State = mapInstanceState(info.State)
		if err := writeVMState(vm); err != nil {
			return VM{}, err
		}

		return vm, nil
	}

	if vm.PID > 0 && processMatches(vm.PID, vm.VMID) {
		m.Logger.WarnContext(ctx, "cloud-hypervisor vm info unavailable",
			slog.String("environment_id", environmentID),
			slog.Int("pid", vm.PID),
			slog.String("socket", vm.SocketPath),
			slog.String("error", clientErr.Error()),
		)

		return vm, nil
	}

	m.cleanupStoppedVM(ctx, vm)

	return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
}

// Remove stops and cleans a VM and its host resources.
func (m Manager) Remove(ctx context.Context, environmentID string) (VM, error) {
	m = m.withDefaults()
	dir := envDir(m.DataDir, environmentID)

	vm, err := readVMState(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return VM{}, err
	}

	if vm.EnvironmentID == "" {
		vm = VM{EnvironmentID: environmentID, EnvDir: dir}
	}

	if vm.PID > 0 && processMatches(vm.PID, vm.VMID) {
		_ = cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vm.shutdown", nil, nil)
		_ = cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vmm.shutdown", nil, nil)

		if err := terminateProcess(vm.PID, 10*time.Second); err != nil {
			return VM{}, err
		}
	}

	m.cleanupStoppedVM(ctx, vm)
	m.Logger.InfoContext(ctx, "removed cloud-hypervisor vm", slog.String("environment_id", environmentID))

	return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
}

func (m Manager) startMachine(
	ctx context.Context,
	environmentID string,
	workspace workspace,
	plan networkPlan,
	resources resolvedResources,
) (VM, error) {
	stdoutPath := filepath.Join(workspace.dir, "stdout.log")

	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // Log path is rooted in the generated environment directory.
	if err != nil {
		return VM{}, fmt.Errorf("open vm stdout log: %w", err)
	}

	defer func() { _ = stdout.Close() }()

	stderrPath := filepath.Join(workspace.dir, "stderr.log")

	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // Log path is rooted in the generated environment directory.
	if err != nil {
		return VM{}, fmt.Errorf("open vm stderr log: %w", err)
	}

	defer func() { _ = stderr.Close() }()

	vmID := shortID(environmentID)

	runtimeBase, err := m.prepareRuntimeLink(workspace.dir, vmID)
	if err != nil {
		return VM{}, err
	}

	socketPath := filepath.Join(runtimeBase, apiSocketName)

	pid, err := startVMMProcess(workspace.assets.cloudHypervisor, socketPath, stdout, stderr)
	if err != nil {
		_ = os.Remove(runtimeBase)

		return VM{}, err
	}

	if err := waitForCloudHypervisorAPI(ctx, socketPath, pid, apiWait); err != nil {
		_ = terminateProcess(pid, vmmStartErrorTimeout)

		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("start cloud-hypervisor API: %w%s", err, logSuffix(stderrPath))
	}

	if err := cloudHypervisorCall(ctx, socketPath, http.MethodPut, "/vm.create", buildVMConfig(workspace, plan, resources.cpus, resources.memoryBytes), nil); err != nil {
		_ = terminateProcess(pid, vmmStartErrorTimeout)
		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("create cloud-hypervisor vm: %w%s", err, logSuffix(stderrPath))
	}

	if err := cloudHypervisorCall(ctx, socketPath, http.MethodPut, "/vm.boot", nil, nil); err != nil {
		_ = terminateProcess(pid, vmmStartErrorTimeout)
		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("boot cloud-hypervisor vm: %w%s", err, logSuffix(stderrPath))
	}

	createdAt := now()
	vm := VM{
		EnvironmentID:   environmentID,
		VMID:            vmID,
		State:           StateCreating,
		PID:             pid,
		EnvDir:          workspace.dir,
		RuntimeDir:      runtimeBase,
		SocketPath:      socketPath,
		VsockSocketPath: vsockSocketPath(workspace),
		KernelPath:      workspace.kernelPath,
		InitramfsPath:   workspace.initramfsPath,
		RootfsPath:      workspace.rootfsPath,
		TapName:         plan.tapName,
		HostIP:          plan.hostIP,
		GuestIP:         plan.guestIP,
		GuestCIDR:       plan.guestCIDR,
		GuestMAC:        plan.guestMAC,
		NetworkIndex:    plan.networkIndex,
		SSHUser:         SSHUser,
		SSHPort:         SSHPort,
		SSHKeyPath:      workspace.sshKeyPath,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}

	if err := m.ensureVMProxyAccess(vm); err != nil {
		_ = terminateProcess(pid, vmmStartErrorTimeout)
		_ = os.Remove(runtimeBase)

		return VM{}, err
	}

	return vm, nil
}

func (m Manager) ensureVMProxyAccess(vm VM) error {
	if vm.VsockSocketPath == "" {
		return nil
	}

	if err := m.ensureProxyDirectoryAccess(filepath.Dir(vm.VsockSocketPath)); err != nil {
		return fmt.Errorf("prepare vsock proxy directories: %w", err)
	}

	if err := m.setProxyAccess(vm.VsockSocketPath, proxySocketMode); err != nil {
		return fmt.Errorf("prepare vsock proxy socket: %w", err)
	}

	return nil
}

func (m Manager) ensureProxyDirectoryAccess(dir string) error {
	if strings.TrimSpace(dir) == "" || dir == "." {
		return nil
	}

	parent := filepath.Dir(dir)
	if parent != dir {
		if err := m.setProxyAccess(parent, proxyDirMode); err != nil {
			return err
		}
	}

	return m.setProxyAccess(dir, proxyDirMode)
}

func (m Manager) setProxyAccess(path string, mode os.FileMode) error {
	uid, gid, configured := m.proxyOwner()
	if configured {
		if err := chownIfNeeded(path, uid, gid); err != nil {
			return err
		}
	}

	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", filepath.Base(path), err)
	}

	return nil
}

func (m Manager) proxyOwner() (int, int, bool) {
	uid, gid := m.ProxyUID, m.ProxyGID
	if uid == 0 && gid == 0 {
		uid, gid = m.UID, m.GID
	}

	return uid, gid, uid != 0 || gid != 0
}

func (m Manager) prepareRuntimeLink(envDir, vmID string) (string, error) {
	if err := os.MkdirAll(runtimeDir, 0o750); err != nil {
		return "", fmt.Errorf("create runtime directory: %w", err)
	}

	runtimeBase := filepath.Join(runtimeDir, vmID)
	if err := os.Remove(runtimeBase); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove stale runtime link: %w", err)
	}

	if err := os.Symlink(envDir, runtimeBase); err != nil {
		return "", fmt.Errorf("create runtime link: %w", err)
	}

	return runtimeBase, nil
}

type cloudHypervisorVMConfig struct {
	CPUs    cloudHypervisorCPUs    `json:"cpus"`
	Memory  cloudHypervisorMemory  `json:"memory"`
	Payload cloudHypervisorPayload `json:"payload"`
	Disks   []cloudHypervisorDisk  `json:"disks"`
	Net     []cloudHypervisorNet   `json:"net"`
	Vsock   cloudHypervisorVsock   `json:"vsock"`
	RNG     cloudHypervisorRNG     `json:"rng"`
	Serial  cloudHypervisorConsole `json:"serial"`
	Console cloudHypervisorConsole `json:"console"`
}

type cloudHypervisorCPUs struct {
	BootVCPUs int  `json:"boot_vcpus"`
	MaxVCPUs  int  `json:"max_vcpus"`
	Nested    bool `json:"nested"`
}

type cloudHypervisorMemory struct {
	Size int64 `json:"size"`
}

type cloudHypervisorPayload struct {
	Kernel    string `json:"kernel"`
	Cmdline   string `json:"cmdline"`
	Initramfs string `json:"initramfs"`
}

type cloudHypervisorDisk struct {
	Path         string `json:"path"`
	Readonly     bool   `json:"readonly,omitempty"`
	ImageType    string `json:"image_type"`
	BackingFiles bool   `json:"backing_files,omitempty"`
}

type cloudHypervisorNet struct {
	Tap  string `json:"tap"`
	IP   string `json:"ip"`
	Mask string `json:"mask"`
	MAC  string `json:"mac"`
}

type cloudHypervisorVsock struct {
	CID    int64  `json:"cid"`
	Socket string `json:"socket"`
}

type cloudHypervisorRNG struct {
	Src string `json:"src"`
}

type cloudHypervisorConsole struct {
	Mode string `json:"mode"`
	File string `json:"file,omitempty"`
}

type cloudHypervisorInfo struct {
	State string `json:"state"`
}

type cloudHypervisorSnapshotConfig struct {
	DestinationURL string `json:"destination_url"`
}

func buildVMConfig(workspace workspace, plan networkPlan, cpus int, memoryBytes int64) cloudHypervisorVMConfig {
	return cloudHypervisorVMConfig{
		CPUs: cloudHypervisorCPUs{
			BootVCPUs: cpus,
			MaxVCPUs:  cpus,
			Nested:    true,
		},
		Memory: cloudHypervisorMemory{Size: memoryBytes},
		Payload: cloudHypervisorPayload{
			Kernel:    workspace.kernelPath,
			Cmdline:   linuxCmdline,
			Initramfs: workspace.initramfsPath,
		},
		Disks: []cloudHypervisorDisk{
			{Path: workspace.rootfsPath, ImageType: "Qcow2", BackingFiles: workspace.rootfsBackingFiles},
			{Path: workspace.seedPath, Readonly: true, ImageType: "Raw"},
		},
		Net: []cloudHypervisorNet{{
			Tap:  plan.tapName,
			IP:   plan.guestIP,
			Mask: "255.255.255.252",
			MAC:  strings.ToLower(plan.guestMAC),
		}},
		Vsock:   cloudHypervisorVsock{CID: vsockCID(plan.networkIndex), Socket: vsockSocketPath(workspace)},
		RNG:     cloudHypervisorRNG{Src: "/dev/urandom"},
		Serial:  cloudHypervisorConsole{Mode: "File", File: filepath.Join(workspace.dir, "serial.log")},
		Console: cloudHypervisorConsole{Mode: "Off"},
	}
}

func vsockSocketPath(workspace workspace) string {
	return filepath.Join(workspace.dir, vsockSocketName)
}

func vsockCID(networkIndex int) int64 {
	return int64(3 + networkIndex)
}

func vmCPUs() int {
	value, err := strconv.Atoi(os.Getenv(vmCPUsEnv))
	if err != nil || value <= 0 {
		return defaultCPUs
	}

	return value
}

func vmMemoryBytes() int64 {
	value, err := strconv.ParseInt(os.Getenv(vmMemoryBytesEnv), 10, 64)
	if err != nil || value <= 0 {
		return defaultMemoryBytes
	}

	return value
}

func startVMMProcess(binaryPath, socketPath string, stdout, stderr *os.File) (int, error) {
	cmd := exec.CommandContext(context.Background(), binaryPath, "--api-socket", "path="+socketPath) //nolint:gosec // Binary path is resolved from the Cloud Hypervisor asset manifest.
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start cloud-hypervisor: %w", err)
	}

	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	return pid, nil
}

func (m Manager) startDHCP(ctx context.Context, dir string, plan networkPlan) (int, error) {
	logPath := filepath.Join(dir, "dnsmasq.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // Log path is rooted in generated VM directory.
	if err != nil {
		return 0, fmt.Errorf("open dnsmasq log: %w", err)
	}

	defer func() { _ = logFile.Close() }()

	args := []string{
		"--no-daemon",
		"--conf-file=",
		"--port=0",
		"--interface=" + plan.tapName,
		"--bind-interfaces",
		"--dhcp-authoritative",
		"--dhcp-range=" + plan.guestIP + "," + plan.guestIP + ",255.255.255.252,1h",
		"--dhcp-option=option:router," + plan.hostIP,
		"--dhcp-option=option:dns-server,8.8.8.8,1.1.1.1",
		"--dhcp-leasefile=" + filepath.Join(dir, "dnsmasq.leases"),
		"--pid-file=" + filepath.Join(dir, "dnsmasq.pid"),
	}

	cmd := exec.CommandContext(context.Background(), "dnsmasq", args...) //nolint:gosec // bastiond intentionally starts dnsmasq for generated TAP networks.
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start dnsmasq: %w", err)
	}

	pid := cmd.Process.Pid

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Wait() }()

	select {
	case err := <-errCh:
		if err != nil {
			return 0, fmt.Errorf("dnsmasq exited: %w%s", err, logSuffix(logPath))
		}

		return 0, fmt.Errorf("dnsmasq exited%s", logSuffix(logPath))
	case <-ctx.Done():
		_ = terminateProcess(pid, vmmStartErrorTimeout)

		return 0, ctx.Err()
	case <-time.After(200 * time.Millisecond):
	}

	return pid, nil
}

func waitForCloudHypervisorAPI(ctx context.Context, socketPath string, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return errors.New("cloud-hypervisor exited before API became available")
		}

		if err := cloudHypervisorCall(ctx, socketPath, http.MethodGet, "/vmm.ping", nil, nil); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	return fmt.Errorf("timed out waiting for cloud-hypervisor API socket %s", socketPath)
}

func cloudHypervisorVMInfo(ctx context.Context, socketPath string) (cloudHypervisorInfo, error) {
	var info cloudHypervisorInfo

	err := cloudHypervisorCall(ctx, socketPath, http.MethodGet, "/vm.info", nil, &info)

	return info, err
}

func (m Manager) prepareGuestForTemplateClone(ctx context.Context, vm VM) error {
	command := strings.Join([]string{
		shellStrictMode,
		"sync",
		"cloud-init clean --logs || true",
		"rm -f /etc/netplan/50-cloud-init.yaml /etc/netplan/50-cloud-init.yaml.bak || true",
		"truncate -s 0 /etc/machine-id || true",
		"rm -f /var/lib/dbus/machine-id || true",
		"sync",
	}, "\n")

	if err := m.runGuestCommand(ctx, vm, command, nil); err != nil {
		return fmt.Errorf("prepare guest for template clone: %w", err)
	}

	return nil
}

func (m Manager) prepareGuestForSnapshot(ctx context.Context, vm VM) error {
	command := strings.Join([]string{
		shellStrictMode,
		"sync",
		"nohup sh -c 'sleep 1; ip addr flush dev eth0 || true; ip link set eth0 down || true; sleep 2; ip link set eth0 up || true; netplan apply || systemctl restart systemd-networkd || systemctl restart networking || dhclient eth0 || true' >/tmp/bastion-resume-network.log 2>&1 &",
	}, "\n")

	if err := m.runGuestCommand(ctx, vm, command, nil); err != nil {
		return fmt.Errorf("prepare guest for snapshot: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(snapshotNetworkDelay):
		return nil
	}
}

func (m Manager) finishTemplateArtifacts(templateID string, workspace workspace, baseContentAddress string) (PreparedTemplate, error) {
	if err := os.RemoveAll(workspace.snapshotPath); err != nil {
		return PreparedTemplate{}, fmt.Errorf("remove stale template snapshot: %w", err)
	}

	for _, path := range []string{
		workspace.seedPath,
		filepath.Join(workspace.dir, "user-data"),
		filepath.Join(workspace.dir, "meta-data"),
		filepath.Join(workspace.dir, "network-config"),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PreparedTemplate{}, fmt.Errorf("remove template boot artifact %s: %w", filepath.Base(path), err)
		}
	}

	if err := os.Chmod(workspace.rootfsPath, 0o400); err != nil {
		return PreparedTemplate{}, fmt.Errorf("mark template rootfs immutable: %w", err)
	}

	createdAt := now()
	prepared := PreparedTemplate{TemplateID: templateID, TemplateDir: workspace.dir, RootfsPath: workspace.rootfsPath, SeedPath: workspace.seedPath, SSHKeyPath: workspace.sshKeyPath, BaseContentAddress: baseContentAddress, CreatedAt: createdAt, UpdatedAt: createdAt}

	if err := writePreparedTemplateMetadata(workspace.dir, prepared); err != nil {
		return PreparedTemplate{}, err
	}

	return prepared, nil
}

func (m Manager) snapshotTemplate(ctx context.Context, templateID string, vm VM, workspace workspace) (PreparedTemplate, error) {
	if err := cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vm.pause", nil, nil); err != nil {
		return PreparedTemplate{}, fmt.Errorf("pause template vm: %w", err)
	}

	if err := os.RemoveAll(workspace.snapshotPath); err != nil {
		return PreparedTemplate{}, fmt.Errorf("remove stale template snapshot: %w", err)
	}

	if err := os.MkdirAll(workspace.snapshotPath, 0o750); err != nil {
		return PreparedTemplate{}, fmt.Errorf("create template snapshot directory: %w", err)
	}

	snapshot := cloudHypervisorSnapshotConfig{DestinationURL: fileURL(workspace.snapshotPath)}
	if err := cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vm.snapshot", snapshot, nil); err != nil {
		return PreparedTemplate{}, fmt.Errorf("snapshot template vm: %w", err)
	}

	if err := os.Chmod(workspace.rootfsPath, 0o400); err != nil {
		return PreparedTemplate{}, fmt.Errorf("mark template rootfs immutable: %w", err)
	}

	createdAt := now()

	return PreparedTemplate{TemplateID: templateID, TemplateDir: workspace.dir, RootfsPath: workspace.rootfsPath, SeedPath: workspace.seedPath, SnapshotDir: workspace.snapshotPath, SSHKeyPath: workspace.sshKeyPath, CreatedAt: createdAt, UpdatedAt: createdAt}, nil
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func templateNetworkID(templateID string) string {
	return "template:" + templateID
}

func cloudHypervisorCall(ctx context.Context, socketPath, method, path string, in, out any) error {
	if socketPath == "" {
		return errors.New("cloud-hypervisor API socket path is required")
	}

	var body io.Reader

	if in != nil {
		contents, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode cloud-hypervisor request: %w", err)
		}

		body = bytes.NewReader(contents)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://localhost/api/v1"+path, body)
	if err != nil {
		return fmt.Errorf("create cloud-hypervisor request: %w", err)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call cloud-hypervisor at %s: %w", socketPath, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		message, _ := io.ReadAll(res.Body)
		return fmt.Errorf("cloud-hypervisor returned %s: %s", res.Status, strings.TrimSpace(string(message)))
	}

	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode cloud-hypervisor response: %w", err)
	}

	return nil
}

func (m Manager) prepareCloudInit(ctx context.Context, environmentID string, workspace workspace, plan networkPlan) error {
	sshKeyPath := workspace.sshKeyPath
	if sshKeyPath == "" {
		sshKeyPath = workspace.assets.sshKey
	}

	publicKey, err := m.publicKeyForSSHKey(ctx, sshKeyPath)
	if err != nil {
		return fmt.Errorf("read SSH public key: %w", err)
	}

	vmID := shortID(environmentID)
	hostname := "bastion-" + strings.TrimPrefix(vmID, "vm-")

	files := map[string]string{
		"user-data":      cloudInitUserData(publicKey),
		"meta-data":      cloudInitMetaData(vmID, hostname),
		"network-config": cloudInitNetworkConfig(plan),
	}

	paths := make([]string, 0, len(files))
	for name, contents := range files {
		path := filepath.Join(workspace.dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return fmt.Errorf("write cloud-init %s: %w", name, err)
		}

		paths = append(paths, path)
	}

	if err := os.Remove(workspace.seedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale cloud-init seed: %w", err)
	}

	if err := m.run(ctx, "mkfs.vfat", "-n", "CIDATA", "-C", workspace.seedPath, "8192"); err != nil {
		return err
	}

	args := append([]string{"-oi", workspace.seedPath}, paths...)
	args = append(args, "::")

	if err := m.run(ctx, "mcopy", args...); err != nil {
		return err
	}

	return nil
}

func (m Manager) publicKeyForSSHKey(ctx context.Context, sshKeyPath string) (string, error) {
	contents, err := os.ReadFile(sshKeyPath + ".pub") //nolint:gosec // SSH key path is resolved from Bastion assets, base artifacts, or an imported prepared template.
	if err == nil {
		return strings.TrimSpace(string(contents)), nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	publicKey, err := m.output(ctx, "ssh-keygen", "-y", "-f", sshKeyPath)
	if err != nil {
		return "", fmt.Errorf("derive SSH public key: %w", err)
	}

	return strings.TrimSpace(publicKey), nil
}

func readPreparedTemplateMetadata(dir string) (PreparedTemplate, error) {
	contents, err := os.ReadFile(filepath.Join(dir, templateMetadataFileName)) //nolint:gosec // Path is rooted in a prepared template directory.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PreparedTemplate{}, fmt.Errorf("prepared template metadata is missing: %w", err)
		}

		return PreparedTemplate{}, fmt.Errorf("read prepared template metadata: %w", err)
	}

	var metadata PreparedTemplate
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return PreparedTemplate{}, fmt.Errorf("parse prepared template metadata: %w", err)
	}

	if strings.TrimSpace(metadata.BaseContentAddress) == "" {
		return PreparedTemplate{}, errors.New("prepared template metadata missing base content address")
	}

	return metadata, nil
}

func writePreparedTemplateMetadata(dir string, metadata PreparedTemplate) error {
	contents, err := json.MarshalIndent(PreparedTemplate{BaseContentAddress: metadata.BaseContentAddress, CreatedAt: metadata.CreatedAt, UpdatedAt: metadata.UpdatedAt}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepared template metadata: %w", err)
	}

	contents = append(contents, '\n')

	if err := atomicWriteFile(filepath.Join(dir, templateMetadataFileName), contents, 0o600); err != nil {
		return fmt.Errorf("write prepared template metadata: %w", err)
	}

	return nil
}

func cloudInitUserData(publicKey string) string {
	return fmt.Sprintf(`#cloud-config
disable_root: false
ssh_pwauth: false
bootcmd:
  - mkdir -p /root/.ssh
  - chmod 700 /root/.ssh
write_files:
  - path: /root/.ssh/authorized_keys
    owner: root:root
    permissions: '0600'
    content: |
      %s
runcmd:
  - sed -i 's/^#*PermitRootLogin .*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
  - systemctl restart ssh || systemctl restart sshd || true
`, publicKey)
}

func cloudInitMetaData(instanceID, hostname string) string {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", instanceID, hostname)
}

func cloudInitNetworkConfig(plan networkPlan) string {
	return fmt.Sprintf(`version: 2
ethernets:
  eth0:
    match:
      macaddress: "%s"
    set-name: eth0
    dhcp4: true
`, strings.ToLower(plan.guestMAC))
}

func logSuffix(path string) string {
	contents, err := os.ReadFile(path) //nolint:gosec // Path is rooted in the generated environment directory.
	if err != nil || len(contents) == 0 {
		return ""
	}

	return ": " + strings.TrimSpace(string(contents))
}

func failVM(vm VM, err error) (VM, error) {
	vm.State = StateError
	vm.LastError = err.Error()

	if writeErr := writeVMState(vm); writeErr != nil {
		return vm, fmt.Errorf("%w; record vm failure: %w", err, writeErr)
	}

	return vm, err
}

func (m Manager) cleanupStoppedVM(ctx context.Context, vm VM) {
	m.cleanupVM(ctx, vm, true)
}

func (m Manager) cleanupVM(ctx context.Context, vm VM, removeDir bool) {
	if vm.PID > 0 && processMatches(vm.PID, vm.VMID) {
		_ = cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vm.shutdown", nil, nil)
		_ = cloudHypervisorCall(ctx, vm.SocketPath, http.MethodPut, "/vmm.shutdown", nil, nil)
		_ = terminateProcess(vm.PID, 10*time.Second)
	}

	if vm.DHCPPID > 0 {
		_ = terminateProcess(vm.DHCPPID, vmmStartErrorTimeout)
	}

	plan := networkPlan{tapName: vm.TapName, networkCIDR: networkCIDR(vm.GuestCIDR), hostIface: vmHostIface(ctx, m)}
	_ = m.cleanupTap(ctx, plan)

	if vm.RuntimeDir != "" {
		_ = os.Remove(vm.RuntimeDir)
	}

	if removeDir && vm.EnvDir != "" {
		_ = os.RemoveAll(vm.EnvDir)
	}
}

func (m Manager) runForVM(ctx context.Context, vm VM, name string, args ...string) error {
	runCtx, stop := vmLivenessContext(ctx, vm)
	defer stop()

	if err := m.run(runCtx, name, args...); err != nil {
		if livenessErr := vmLivenessError(runCtx); livenessErr != nil {
			return livenessErr
		}

		return err
	}

	return nil
}

func vmLivenessContext(ctx context.Context, vm VM) (context.Context, func()) {
	if vm.PID <= 0 {
		return ctx, func() {}
	}

	livenessCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(vmLivenessPoll)
		defer ticker.Stop()

		for {
			if !processMatches(vm.PID, vm.VMID) {
				cancel(vmProcessExitError{environmentID: vm.EnvironmentID, pid: vm.PID})

				return
			}

			select {
			case <-livenessCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return livenessCtx, func() {
		cancel(context.Canceled)
		<-done
	}
}

func vmLivenessError(ctx context.Context) error {
	err := context.Cause(ctx)
	if errors.Is(err, errVMProcessExited) {
		return err
	}

	return nil
}

var errVMProcessExited = errors.New("cloud-hypervisor process exited")

type vmProcessExitError struct {
	environmentID string
	pid           int
}

func (e vmProcessExitError) Error() string {
	if e.environmentID != "" {
		return "cloud-hypervisor process exited for VM " + e.environmentID
	}

	return "cloud-hypervisor process exited for pid " + strconv.Itoa(e.pid)
}

func (e vmProcessExitError) Unwrap() error {
	return errVMProcessExited
}

func (m Manager) withDefaults() Manager {
	if m.Logger == nil {
		m.Logger = slog.New(slog.DiscardHandler)
	}

	if m.run == nil {
		m.run = runCommand
	}

	if m.stream == nil {
		m.stream = runCommandStream
	}

	if m.output == nil {
		m.output = outputCommand
	}

	return m
}

func mapInstanceState(state string) string {
	switch state {
	case "Running":
		return StateRunning
	case "Paused":
		return StatePaused
	default:
		return StateStopped
	}
}

func shortID(environmentID string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(environmentID))

	encoded := make([]byte, 4)
	binary.BigEndian.PutUint32(encoded, hash.Sum32())

	return "vm-" + hex.EncodeToString(encoded)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // Source is resolved from the Cloud Hypervisor asset manifest.
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(src), err)
	}
	defer func() { _ = in.Close() }()

	//nolint:gosec // Destination is rooted in the generated environment directory.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(dst), err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy %s: %w", filepath.Base(dst), err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(dst), err)
	}

	return nil
}

func chownIfConfigured(path string, uid, gid int) error {
	if uid == 0 && gid == 0 {
		return nil
	}

	return chownIfNeeded(path, uid, gid)
}

func chownIfNeeded(path string, uid, gid int) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) == uid && int(stat.Gid) == gid {
		return nil
	}

	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", filepath.Base(path), err)
	}

	return nil
}

func processExists(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func processMatches(pid int, vmID string) bool {
	if !processExists(pid) {
		return false
	}

	contents, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}

	cmdline := strings.ReplaceAll(string(contents), "\x00", " ")

	return strings.Contains(cmdline, vmID) || strings.Contains(cmdline, cloudHypervisorName)
}

func terminateProcess(pid int, timeout time.Duration) error {
	if !processExists(pid) {
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate cloud-hypervisor pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill cloud-hypervisor pid %d: %w", pid, err)
	}

	return nil
}

func networkCIDR(guestCIDR string) string {
	if guestCIDR == "" {
		return ""
	}

	ip, network, err := net.ParseCIDR(guestCIDR)
	if err != nil {
		return ""
	}

	network.IP = ip.Mask(network.Mask)

	return network.String()
}

func vmHostIface(ctx context.Context, m Manager) string {
	iface, err := m.defaultRouteInterface(ctx)
	if err != nil {
		return ""
	}

	return iface
}
