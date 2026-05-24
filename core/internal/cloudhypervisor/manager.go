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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	apiSocketName        = "api.socket"
	runtimeDir           = "/run/bastion/vms"
	sshWait              = 180 * time.Second
	apiWait              = 15 * time.Second
	vmmStartErrorTimeout = 5 * time.Second
	vmCPUsEnv            = "BASTION_VM_CPUS"
	vmMemoryBytesEnv     = "BASTION_VM_MEMORY_BYTES"
	linuxCmdline         = "root=LABEL=cloudimg-rootfs rootwait ro console=ttyS0"
	defaultCPUs          = 2
	defaultMemoryBytes   = 2 << 30
	defaultRootfsSize    = "20G"
	gibBytes             = int64(1 << 30)
)

// Manager performs privileged Cloud Hypervisor operations for bastiond.
type Manager struct {
	DataDir string
	UID     int
	GID     int
	Logger  *slog.Logger
	run     func(context.Context, string, ...string) error
	stream  func(context.Context, io.Writer, string, ...string) error
	output  func(context.Context, string, ...string) (string, error)
}

// NewManager returns a Cloud Hypervisor VM manager.
func NewManager(dataDir string, uid, gid int, logger *slog.Logger) Manager {
	return Manager{
		DataDir: dataDir,
		UID:     uid,
		GID:     gid,
		Logger:  logger,
		run:     runCommand,
		stream:  runCommandStream,
		output:  outputCommand,
	}
}

// Launch creates and starts a Cloud Hypervisor VM.
func (m Manager) Launch(ctx context.Context, req LaunchRequest) (VM, error) {
	m = m.withDefaults()

	if strings.TrimSpace(req.EnvironmentID) == "" {
		return VM{}, errors.New("environment id is required")
	}

	templateResources, err := parseTemplateResources(req.Template.Config)
	if err != nil {
		return VM{}, err
	}

	resources, err := templateResources.resolve()
	if err != nil {
		return VM{}, err
	}

	workspace, err := m.prepareWorkspace(ctx, req.EnvironmentID, resources)
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

	if err := m.prepareCloudInit(ctx, req.EnvironmentID, workspace, plan); err != nil {
		_ = m.cleanupTap(context.Background(), plan)
		_ = os.RemoveAll(workspace.dir)

		return VM{}, err
	}

	vm, err := m.startMachine(ctx, req.EnvironmentID, workspace, plan, resources)
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

	if err := m.runInitActions(ctx, vm, req.Template.Config, req.Logs); err != nil {
		return failVM(vm, err)
	}

	vm.State = StateRunning
	if err := writeVMState(vm); err != nil {
		_, _ = m.Remove(context.Background(), req.EnvironmentID)

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

type workspace struct {
	dir           string
	rootfsPath    string
	seedPath      string
	kernelPath    string
	initramfsPath string
	assets        assets
}

func (m Manager) prepareWorkspace(ctx context.Context, environmentID string, resources resolvedResources) (workspace, error) {
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

	rootfsPath := filepath.Join(dir, envRootfsFileName)
	seedPath := filepath.Join(dir, envSeedFileName)

	if err := copyFile(assetSet.rootfs, rootfsPath, 0o640); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := m.run(ctx, "qemu-img", "resize", rootfsPath, resources.rootfsSize); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	if err := chownIfConfigured(rootfsPath, m.UID, m.GID); err != nil {
		_ = os.RemoveAll(dir)

		return workspace{}, err
	}

	return workspace{dir: dir, rootfsPath: rootfsPath, seedPath: seedPath, kernelPath: assetSet.kernel, initramfsPath: assetSet.initramfs, assets: assetSet}, nil
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

	return VM{
		EnvironmentID: environmentID,
		VMID:          vmID,
		State:         StateCreating,
		PID:           pid,
		EnvDir:        workspace.dir,
		RuntimeDir:    runtimeBase,
		SocketPath:    socketPath,
		KernelPath:    workspace.kernelPath,
		InitramfsPath: workspace.initramfsPath,
		RootfsPath:    workspace.rootfsPath,
		TapName:       plan.tapName,
		HostIP:        plan.hostIP,
		GuestIP:       plan.guestIP,
		GuestCIDR:     plan.guestCIDR,
		GuestMAC:      plan.guestMAC,
		NetworkIndex:  plan.networkIndex,
		SSHUser:       SSHUser,
		SSHPort:       SSHPort,
		SSHKeyPath:    workspace.assets.sshKey,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}, nil
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
	Path      string `json:"path"`
	Readonly  bool   `json:"readonly,omitempty"`
	ImageType string `json:"image_type"`
}

type cloudHypervisorNet struct {
	Tap  string `json:"tap"`
	IP   string `json:"ip"`
	Mask string `json:"mask"`
	MAC  string `json:"mac"`
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
			{Path: workspace.rootfsPath, ImageType: "Qcow2"},
			{Path: workspace.seedPath, Readonly: true, ImageType: "Raw"},
		},
		Net: []cloudHypervisorNet{{
			Tap:  plan.tapName,
			IP:   plan.guestIP,
			Mask: "255.255.255.252",
			MAC:  strings.ToLower(plan.guestMAC),
		}},
		RNG:     cloudHypervisorRNG{Src: "/dev/urandom"},
		Serial:  cloudHypervisorConsole{Mode: "File", File: filepath.Join(workspace.dir, "serial.log")},
		Console: cloudHypervisorConsole{Mode: "Off"},
	}
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

	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}

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
	publicKey, err := os.ReadFile(workspace.assets.sshKey + ".pub")
	if err != nil {
		return fmt.Errorf("read SSH public key: %w", err)
	}

	vmID := shortID(environmentID)
	hostname := "bastion-" + strings.TrimPrefix(vmID, "vm-")

	files := map[string]string{
		"user-data":      cloudInitUserData(strings.TrimSpace(string(publicKey))),
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
    dhcp4: false
    addresses:
      - %s
    routes:
      - to: default
        via: %s
    nameservers:
      addresses:
        - 8.8.8.8
        - 1.1.1.1
`, strings.ToLower(plan.guestMAC), plan.guestCIDR, plan.hostIP)
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
	plan := networkPlan{tapName: vm.TapName, networkCIDR: networkCIDR(vm.GuestCIDR), hostIface: vmHostIface(ctx, m)}
	_ = m.cleanupTap(ctx, plan)

	if vm.RuntimeDir != "" {
		_ = os.Remove(vm.RuntimeDir)
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
