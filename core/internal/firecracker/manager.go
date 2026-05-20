package firecracker

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	fc "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"golang.org/x/sys/unix"
)

const (
	apiSocketName        = "api.socket"
	runtimeDir           = "/run/bastion/vms"
	sdkInitTimeoutEnv    = "FIRECRACKER_GO_SDK_INIT_TIMEOUT_SECONDS"
	sdkInitTimeoutValue  = "15"
	sshWait              = 90 * time.Second
	vmmStartErrorTimeout = 5 * time.Second
)

// Manager performs privileged Firecracker operations for bastiond.
type Manager struct {
	DataDir string
	UID     int
	GID     int
	Logger  *slog.Logger
	run     func(context.Context, string, ...string) error
	output  func(context.Context, string, ...string) (string, error)
}

// NewManager returns a Firecracker VM manager.
func NewManager(dataDir string, uid, gid int, logger *slog.Logger) Manager {
	return Manager{
		DataDir: dataDir,
		UID:     uid,
		GID:     gid,
		Logger:  logger,
		run:     runCommand,
		output:  outputCommand,
	}
}

// Launch creates and starts a jailed Firecracker VM.
func (m Manager) Launch(ctx context.Context, req LaunchRequest) (VM, error) {
	m = m.withDefaults()

	if strings.TrimSpace(req.EnvironmentID) == "" {
		return VM{}, errors.New("environment id is required")
	}

	workspace, err := m.prepareWorkspace(req.EnvironmentID)
	if err != nil {
		return VM{}, err
	}

	plan, err := planNetwork(req.EnvironmentID)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	plan, err = m.setupTap(ctx, plan)
	if err != nil {
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	vm, err := m.startMachine(req.EnvironmentID, workspace, plan)
	if err != nil {
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	if err := writeVMState(vm); err != nil {
		_, _ = m.Remove(context.Background(), req.EnvironmentID)

		return VM{}, err
	}

	if err := waitForTCP(ctx, vm.GuestIP, vm.SSHPort, sshWait); err != nil {
		vm.State = StateError
		vm.LastError = err.Error()
		_ = writeVMState(vm)
		_, _ = m.Remove(context.Background(), req.EnvironmentID)

		return VM{}, err
	}

	vm.State = StateRunning
	if err := writeVMState(vm); err != nil {
		_, _ = m.Remove(context.Background(), req.EnvironmentID)

		return VM{}, err
	}

	m.Logger.InfoContext(ctx, "launched firecracker vm",
		slog.String("environment_id", vm.EnvironmentID),
		slog.String("vm_id", vm.VMID),
		slog.Int("pid", vm.PID),
		slog.String("guest_ip", vm.GuestIP),
		slog.String("tap", vm.TapName),
	)

	return vm, nil
}

type workspace struct {
	dir        string
	rootfsPath string
	kernelPath string
	assets     assets
}

func (m Manager) prepareWorkspace(environmentID string) (workspace, error) {
	assetSet, err := loadAssets(m.DataDir)
	if err != nil {
		return workspace{}, err
	}

	dir := envDir(m.DataDir, environmentID)
	if err := os.RemoveAll(dir); err != nil {
		return workspace{}, fmt.Errorf("remove stale environment directory: %w", err)
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return workspace{}, fmt.Errorf("create environment directory: %w", err)
	}

	if err := ensureDeviceNodesAllowed(dir); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	rootfsPath := filepath.Join(dir, envRootfsFileName)
	kernelPath := filepath.Join(dir, envKernelFileName)

	if err := copyFile(assetSet.rootfs, rootfsPath, 0o640); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := copyFile(assetSet.kernel, kernelPath, 0o640); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := chownIfConfigured(rootfsPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := chownIfConfigured(kernelPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	return workspace{dir: dir, rootfsPath: rootfsPath, kernelPath: kernelPath, assets: assetSet}, nil
}

// State reconciles durable VM state with the running Firecracker process.
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

	if vm.PID == 0 || !processExists(vm.PID) || vm.SocketPath == "" {
		m.cleanupStoppedVM(ctx, vm)

		return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
	}

	client := fc.NewClient(vm.SocketPath, nil, false)

	info, clientErr := client.GetInstanceInfo(ctx)
	if clientErr == nil {
		vm.State = mapInstanceState(fc.StringValue(info.Payload.State))
		if err := writeVMState(vm); err != nil {
			return VM{}, err
		}

		return vm, nil
	}

	if vm.PID > 0 && processMatches(vm.PID, vm.VMID) {
		_ = terminateProcess(vm.PID, 5*time.Second)
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
		if err := terminateProcess(vm.PID, 10*time.Second); err != nil {
			return VM{}, err
		}
	}

	m.cleanupStoppedVM(ctx, vm)
	m.Logger.InfoContext(ctx, "removed firecracker vm", slog.String("environment_id", environmentID))

	return VM{EnvironmentID: environmentID, State: StateStopped, EnvDir: dir, UpdatedAt: now()}, nil
}

//nolint:funlen // Keeping the Firecracker SDK config together makes launch behavior easier to audit.
func (m Manager) startMachine(
	environmentID string,
	workspace workspace,
	plan networkPlan,
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

	guestIPNet, err := ipNet(plan.guestCIDR)
	if err != nil {
		return VM{}, err
	}

	uid := m.UID
	gid := m.GID
	numaNode := 0
	vmID := shortID(environmentID)

	runtimeBase, jailerDir, err := m.prepareRuntimeLink(workspace.dir, vmID)
	if err != nil {
		return VM{}, err
	}

	if err := os.MkdirAll(jailerDir, 0o750); err != nil {
		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("create jailer directory: %w", err)
	}

	cfg := fc.Config{
		SocketPath:      apiSocketName,
		KernelImagePath: workspace.kernelPath,
		KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off nomodules rw",
		Drives:          fc.NewDrivesBuilder(workspace.rootfsPath).Build(),
		NetworkInterfaces: []fc.NetworkInterface{{
			StaticConfiguration: &fc.StaticNetworkConfiguration{
				MacAddress:  plan.guestMAC,
				HostDevName: plan.tapName,
				IPConfiguration: &fc.IPConfiguration{
					IPAddr:      guestIPNet,
					Gateway:     net.ParseIP(plan.hostIP),
					Nameservers: []string{"8.8.8.8"},
				},
			},
		}},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  fc.Int64(1),
			MemSizeMib: fc.Int64(512),
		},
		// VMs are durable runtime state; bastiond restarts must not forward signals into the VMM.
		ForwardSignals: []os.Signal{},
		JailerCfg: &fc.JailerConfig{
			UID:            &uid,
			GID:            &gid,
			ID:             vmID,
			NumaNode:       &numaNode,
			ExecFile:       workspace.assets.firecracker,
			JailerBinary:   workspace.assets.jailer,
			ChrootBaseDir:  jailerDir,
			CgroupVersion:  "2",
			ChrootStrategy: fc.NewNaiveChrootStrategy(workspace.kernelPath),
			Stdout:         stdout,
			Stderr:         stderr,
		},
		VMID: vmID,
	}

	if os.Getenv(sdkInitTimeoutEnv) == "" {
		_ = os.Setenv(sdkInitTimeoutEnv, sdkInitTimeoutValue)
	}

	machine, err := fc.NewMachine(context.Background(), cfg)
	if err != nil {
		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("create firecracker machine: %w", err)
	}

	if err := machine.Start(context.Background()); err != nil {
		stopStartedMachine(machine)

		_ = os.Remove(runtimeBase)

		return VM{}, fmt.Errorf("start firecracker machine: %w%s", err, logSuffix(stderrPath))
	}

	pid, err := machine.PID()
	if err != nil {
		return VM{}, fmt.Errorf("get firecracker pid: %w", err)
	}

	createdAt := now()

	return VM{
		EnvironmentID: environmentID,
		VMID:          vmID,
		State:         StateCreating,
		PID:           pid,
		EnvDir:        workspace.dir,
		JailerDir:     jailerDir,
		SocketPath:    machine.Cfg.SocketPath,
		KernelPath:    workspace.kernelPath,
		RootfsPath:    workspace.rootfsPath,
		TapName:       plan.tapName,
		HostIP:        plan.hostIP,
		GuestIP:       plan.guestIP,
		GuestCIDR:     plan.guestCIDR,
		GuestMAC:      plan.guestMAC,
		SSHUser:       SSHUser,
		SSHPort:       SSHPort,
		SSHKeyPath:    workspace.assets.sshKey,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}, nil
}

func (m Manager) prepareRuntimeLink(envDir, vmID string) (string, string, error) {
	if err := os.MkdirAll(runtimeDir, 0o750); err != nil {
		return "", "", fmt.Errorf("create runtime directory: %w", err)
	}

	runtimeBase := filepath.Join(runtimeDir, vmID)
	if err := os.Remove(runtimeBase); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("remove stale runtime link: %w", err)
	}

	if err := os.Symlink(envDir, runtimeBase); err != nil {
		return "", "", fmt.Errorf("create runtime link: %w", err)
	}

	return runtimeBase, filepath.Join(runtimeBase, "j"), nil
}

func stopStartedMachine(machine *fc.Machine) {
	pid, err := machine.PID()
	if err != nil {
		_ = machine.StopVMM()

		return
	}

	_ = terminateProcess(pid, vmmStartErrorTimeout)
}

func logSuffix(path string) string {
	contents, err := os.ReadFile(path) //nolint:gosec // Path is rooted in the generated environment directory.
	if err != nil || len(contents) == 0 {
		return ""
	}

	return ": " + strings.TrimSpace(string(contents))
}

func (m Manager) cleanupStoppedVM(ctx context.Context, vm VM) {
	plan := networkPlan{tapName: vm.TapName, networkCIDR: networkCIDR(vm.GuestCIDR), hostIface: vmHostIface(ctx, m)}
	_ = m.cleanupTap(ctx, plan)

	if vm.JailerDir != "" {
		_ = os.Remove(filepath.Dir(vm.JailerDir))
	}

	if vm.EnvDir != "" {
		_ = os.RemoveAll(vm.EnvDir)
	}
}

func (m Manager) withDefaults() Manager {
	if m.Logger == nil {
		m.Logger = slog.New(slog.DiscardHandler)
	}

	if m.run == nil {
		m.run = runCommand
	}

	if m.output == nil {
		m.output = outputCommand
	}

	return m
}

func mapInstanceState(state string) string {
	switch state {
	case models.InstanceInfoStateRunning:
		return StateRunning
	case models.InstanceInfoStatePaused:
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
	in, err := os.Open(src) //nolint:gosec // Source is resolved from the Firecracker asset manifest.
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

	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", filepath.Base(path), err)
	}

	return nil
}

func ensureDeviceNodesAllowed(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return fmt.Errorf("inspect environment directory mount: %w", err)
	}

	if stat.Flags&unix.ST_NODEV != 0 {
		return fmt.Errorf("environment directory %s is on a nodev mount; Firecracker jailer requires a data directory on a filesystem that allows device nodes", path)
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

	return strings.Contains(cmdline, vmID) || strings.Contains(cmdline, firecrackerName)
}

func terminateProcess(pid int, timeout time.Duration) error {
	if !processExists(pid) {
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate firecracker pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill firecracker pid %d: %w", pid, err)
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
