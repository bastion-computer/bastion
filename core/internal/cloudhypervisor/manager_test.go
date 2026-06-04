package cloudhypervisor

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareCloudInitUsesEnvironmentIDForHostname(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	sshKeyPath := filepath.Join(dir, "id_rsa")

	if err := os.WriteFile(sshKeyPath+".pub", []byte("ssh-ed25519 test-key\n"), 0o600); err != nil {
		t.Fatalf("write ssh public key: %v", err)
	}

	manager := Manager{run: func(context.Context, string, ...string) error { return nil }}
	workspace := workspace{
		dir:      dir,
		seedPath: filepath.Join(dir, envSeedFileName),
		assets:   assets{sshKey: sshKeyPath},
	}

	plan := networkPlan{guestMAC: "06:00:0A:F1:00:02", guestCIDR: "10.241.0.2/30", hostIP: "10.241.0.1"}

	const environmentID = "env_hostname"

	if err := manager.prepareCloudInit(context.Background(), environmentID, workspace, plan); err != nil {
		t.Fatalf("prepare cloud-init: %v", err)
	}

	metadataPath := filepath.Join(dir, "meta-data")

	contents, err := os.ReadFile(metadataPath) //nolint:gosec // Test reads cloud-init metadata generated inside t.TempDir.
	if err != nil {
		t.Fatalf("read meta-data: %v", err)
	}

	vmID := shortID(environmentID)

	want := "instance-id: " + vmID + "\nlocal-hostname: bastion-" + strings.TrimPrefix(vmID, "vm-") + "\n"
	if string(contents) != want {
		t.Fatalf("meta-data = %q, want %q", contents, want)
	}
}

func TestPrepareWorkspaceUsesResourceVolumeSize(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestCloudHypervisorAssets(t, dataDir)

	rootfsSize := strconv.FormatInt(5*gibBytes, 10)

	var resizeArgs []string

	manager := Manager{DataDir: dataDir, run: func(_ context.Context, name string, args ...string) error {
		if name == "qemu-img" {
			resizeArgs = append([]string(nil), args...)
		}

		return nil
	}}

	workspace, err := manager.prepareWorkspace(context.Background(), "env_resources", resolvedResources{rootfsSize: rootfsSize})
	if err != nil {
		t.Fatalf("prepare workspace: %v", err)
	}

	if len(resizeArgs) != 3 || resizeArgs[0] != "resize" || resizeArgs[1] != workspace.rootfsPath || resizeArgs[2] != rootfsSize {
		t.Fatalf("qemu-img resize args = %#v, want resize rootfs to resource volume", resizeArgs)
	}
}

func TestBuildVMConfigUsesResolvedCPUAndMemory(t *testing.T) {
	t.Parallel()

	workspace := workspace{
		dir:           t.TempDir(),
		rootfsPath:    "rootfs.img",
		seedPath:      "cidata.img",
		kernelPath:    "vmlinux",
		initramfsPath: "initramfs.img",
	}
	plan := networkPlan{tapName: "bt123", guestIP: "10.241.0.2", guestMAC: "06:00:0A:F1:00:02"}

	config := buildVMConfig(workspace, plan, 3, 4*gibBytes)
	if config.CPUs.BootVCPUs != 3 || config.CPUs.MaxVCPUs != 3 || config.Memory.Size != 4*gibBytes {
		t.Fatalf("vm config resources = cpu %#v memory %#v, want template resources", config.CPUs, config.Memory)
	}
}

func TestStateKeepsLiveVMWhenInfoProbeFails(t *testing.T) {
	t.Parallel()

	const environmentID = "env_state_probe"

	dataDir := t.TempDir()

	dir := envDir(dataDir, environmentID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create environment dir: %v", err)
	}

	cmd := startTestCloudHypervisorProcess(t)
	vm := VM{
		EnvironmentID: environmentID,
		VMID:          "vm-state-probe",
		State:         StateRunning,
		PID:           cmd.Process.Pid,
		EnvDir:        dir,
		SocketPath:    filepath.Join(dir, "missing.socket"),
	}

	if err := writeVMState(vm); err != nil {
		t.Fatalf("write vm state: %v", err)
	}

	manager := Manager{DataDir: dataDir, run: func(context.Context, string, ...string) error { return nil }}

	got, err := manager.State(context.Background(), environmentID)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if got.State != StateRunning {
		t.Fatalf("state = %q, want %q", got.State, StateRunning)
	}

	if !processExists(cmd.Process.Pid) {
		t.Fatalf("test cloud-hypervisor process was terminated")
	}

	if _, err := os.Stat(statePath(dir)); err != nil {
		t.Fatalf("vm state file was removed: %v", err)
	}
}

func TestCloudHypervisorCallClosesIdleConnections(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "api.socket")

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen on test socket: %v", err)
	}

	var openConnections atomic.Int64

	server := &http.Server{ //nolint:gosec // Test server only listens on a private Unix socket.
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"Running"}`))
		}),
		ConnState: func(_ net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				openConnections.Add(1)
			case http.StateClosed:
				openConnections.Add(-1)
			case http.StateActive, http.StateIdle, http.StateHijacked:
			}
		},
	}

	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() { _ = server.Close() })

	for range 20 {
		var info cloudHypervisorInfo
		if err := cloudHypervisorCall(context.Background(), socketPath, http.MethodGet, "/vm.info", nil, &info); err != nil {
			t.Fatalf("call cloud-hypervisor: %v", err)
		}

		if info.State != "Running" {
			t.Fatalf("vm info state = %q, want Running", info.State)
		}
	}

	for range 100 {
		if openConnections.Load() == 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("open cloud-hypervisor API connections = %d, want 0", openConnections.Load())
}

func startTestCloudHypervisorProcess(t *testing.T) *exec.Cmd {
	t.Helper()

	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("find sleep: %v", err)
	}

	link := filepath.Join(t.TempDir(), cloudHypervisorName)
	if err := os.Symlink(sleepPath, link); err != nil {
		t.Fatalf("create cloud-hypervisor test process link: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), link, "30") //nolint:gosec // Test controls the symlink target.
	if err := cmd.Start(); err != nil {
		t.Fatalf("start test cloud-hypervisor process: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	for range 100 {
		if processMatches(cmd.Process.Pid, cloudHypervisorName) {
			return cmd
		}

		if !processExists(cmd.Process.Pid) {
			t.Fatalf("test cloud-hypervisor process exited before cmdline was visible")
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("test cloud-hypervisor process cmdline did not become visible")

	return cmd
}

func writeTestCloudHypervisorAssets(t *testing.T, dataDir string) {
	t.Helper()

	assetDir := filepath.Join(dataDir, assetDirName)
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		t.Fatalf("create asset dir: %v", err)
	}

	files := []struct {
		name string
		mode os.FileMode
	}{
		{name: cloudHypervisorName, mode: 0o700},
		{name: "vmlinux", mode: 0o600},
		{name: "initramfs.img", mode: 0o600},
		{name: "rootfs.img", mode: 0o600},
		{name: "id_rsa", mode: 0o600},
	}

	for _, file := range files {
		if err := os.WriteFile(filepath.Join(assetDir, file.name), []byte("test"), file.mode); err != nil {
			t.Fatalf("write asset %s: %v", file.name, err)
		}
	}

	manifest := `{"cloud_hypervisor":"cloud-hypervisor","kernel":"vmlinux","initramfs":"initramfs.img","rootfs_image":"rootfs.img","ssh_key":"id_rsa"}`
	if err := os.WriteFile(filepath.Join(assetDir, manifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write asset manifest: %v", err)
	}
}
