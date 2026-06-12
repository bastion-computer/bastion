package cloudhypervisor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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

func TestCloudInitNetworkConfigUsesDHCP(t *testing.T) {
	t.Parallel()

	config := cloudInitNetworkConfig(networkPlan{guestMAC: "06:00:0A:F1:00:02", guestCIDR: "10.241.0.2/30", hostIP: "10.241.0.1"})

	for _, want := range []string{"dhcp4: true", "eth0:", "match:", `macaddress: "06:00:0a:f1:00:02"`, "set-name: eth0"} {
		if !strings.Contains(config, want) {
			t.Fatalf("network config = %q, want %q", config, want)
		}
	}

	for _, forbidden := range []string{"dhcp4: false", "addresses:", "routes:"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("network config = %q, must not contain %q", config, forbidden)
		}
	}
}

func TestPrepareTemplateWorkspaceUsesResourceVolumeSize(t *testing.T) {
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

	workspace, err := manager.prepareTemplateWorkspace(context.Background(), "tpl_resources", resolvedResources{rootfsSize: rootfsSize})
	if err != nil {
		t.Fatalf("prepare workspace: %v", err)
	}

	if len(resizeArgs) != 3 || resizeArgs[0] != "resize" || resizeArgs[1] != workspace.rootfsPath || resizeArgs[2] != rootfsSize {
		t.Fatalf("qemu-img resize args = %#v, want resize rootfs to resource volume", resizeArgs)
	}
}

func TestPrepareRestoreWorkspaceCreatesQCOW2Overlay(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestCloudHypervisorAssets(t, dataDir)

	templateID := "tpl_overlay"
	templateRootfs := filepath.Join(templateDir(dataDir, templateID), envRootfsFileName)
	writeTestPreparedTemplate(t, dataDir, templateID)

	var qemuImgArgs []string

	manager := Manager{DataDir: dataDir, run: func(_ context.Context, name string, args ...string) error {
		if name == "qemu-img" {
			qemuImgArgs = append([]string(nil), args...)
		}

		return nil
	}}

	workspace, err := manager.prepareRestoreWorkspace(context.Background(), "env_overlay", Template{ID: templateID})
	if err != nil {
		t.Fatalf("prepare restore workspace: %v", err)
	}

	want := []string{"create", "-f", "qcow2", "-F", "qcow2", "-b", templateRootfs, workspace.rootfsPath}
	if !slicesEqual(qemuImgArgs, want) {
		t.Fatalf("qemu-img args = %#v, want %#v", qemuImgArgs, want)
	}
}

func TestPatchSnapshotConfigUsesEnvironmentDiskAndNetwork(t *testing.T) {
	t.Parallel()

	workspace := workspace{dir: t.TempDir(), rootfsPath: "/env/rootfs.img"}
	plan := networkPlan{networkIndex: 3, tapName: "bt123", guestIP: "10.241.0.6", guestMAC: "06:00:0A:F1:00:06"}
	input := []byte(`{
  "disks": [
    {"path":"/template/rootfs.img","image_type":"Qcow2"},
    {"path":"/template/cidata.img","readonly":true,"image_type":"Raw"}
  ],
  "net": [{"tap":"btold","ip":"10.241.0.2","mask":"255.255.255.252","mac":"06:00:0A:F1:00:02"}],
  "vsock": {"cid": 3, "socket":"/template/vsock.socket"},
  "serial": {"mode":"File","file":"/template/serial.log"}
}`)

	patched, err := patchSnapshotConfig(input, workspace, plan)
	if err != nil {
		t.Fatalf("patch snapshot config: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatalf("unmarshal patched config: %v", err)
	}

	disks := requireJSONArray(t, got["disks"], "disks")

	rootfs := requireJSONObject(t, disks[0], "rootfs disk")
	if rootfs["path"] != workspace.rootfsPath || rootfs["backing_files"] != true {
		t.Fatalf("rootfs disk = %#v, want env overlay with backing files", rootfs)
	}

	nets := requireJSONArray(t, got["net"], "net")

	net := requireJSONObject(t, nets[0], "net config")
	if net["tap"] != plan.tapName || net["ip"] != plan.guestIP || net["mac"] != strings.ToLower(plan.guestMAC) {
		t.Fatalf("net config = %#v, want env network", net)
	}

	serial := requireJSONObject(t, got["serial"], "serial config")
	if serial["file"] != filepath.Join(workspace.dir, "serial.log") {
		t.Fatalf("serial config = %#v, want env serial log", serial)
	}

	vsock := requireJSONObject(t, got["vsock"], "vsock config")
	if vsock["cid"] != float64(vsockCID(plan.networkIndex)) || vsock["socket"] != filepath.Join(workspace.dir, vsockSocketName) {
		t.Fatalf("vsock config = %#v, want env vsock socket", vsock)
	}
}

func requireJSONArray(t *testing.T, value any, name string) []any {
	t.Helper()

	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("%s = %#v, want non-empty array", name, value)
	}

	return items
}

func requireJSONObject(t *testing.T, value any, name string) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, value)
	}

	return object
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
	plan := networkPlan{tapName: "bt123", guestIP: "10.241.0.9", guestMAC: "06:00:0A:F1:00:09"}

	config := buildVMConfig(workspace, plan, 3, 4*gibBytes)
	if config.CPUs.BootVCPUs != 3 || config.CPUs.MaxVCPUs != 3 || config.Memory.Size != 4*gibBytes {
		t.Fatalf("vm config resources = cpu %#v memory %#v, want template resources", config.CPUs, config.Memory)
	}
}

func TestBuildVMConfigIncludesVsockDevice(t *testing.T) {
	t.Parallel()

	workspace := workspace{
		dir:           t.TempDir(),
		rootfsPath:    "rootfs.img",
		seedPath:      "cidata.img",
		kernelPath:    "vmlinux",
		initramfsPath: "initramfs.img",
	}
	plan := networkPlan{networkIndex: 7, tapName: "bt123", guestIP: "10.241.0.2", guestMAC: "06:00:0A:F1:00:02"}

	config := buildVMConfig(workspace, plan, 2, 2*gibBytes)
	if config.Vsock.CID != vsockCID(plan.networkIndex) || config.Vsock.Socket != filepath.Join(workspace.dir, vsockSocketName) {
		t.Fatalf("vsock config = %#v, want CID/socket for environment", config.Vsock)
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

func TestEnsureVMProxyAccessSetsVsockSocketAccess(t *testing.T) {
	t.Parallel()

	const environmentID = "env_vsock_access"

	dataDir := t.TempDir()

	dir := envDir(dataDir, environmentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create environment dir: %v", err)
	}

	environmentsPath := filepath.Dir(dir)
	//nolint:gosec // Test intentionally restricts directory traversal before repair.
	if err := os.Chmod(environmentsPath, 0o700); err != nil {
		t.Fatalf("restrict environments dir: %v", err)
	}

	vsockPath := filepath.Join(dir, vsockSocketName)

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", vsockPath)
	if err != nil {
		t.Fatalf("listen on test vsock socket: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	if err := os.Chmod(vsockPath, 0o600); err != nil {
		t.Fatalf("restrict test vsock socket: %v", err)
	}

	uid, gid := os.Getuid(), os.Getgid()
	if os.Geteuid() == 0 {
		uid, gid = 12345, 12345
	}

	manager := Manager{DataDir: dataDir, ProxyUID: uid, ProxyGID: gid, run: func(context.Context, string, ...string) error { return nil }}
	if err := manager.ensureVMProxyAccess(VM{VsockSocketPath: vsockPath}); err != nil {
		t.Fatalf("ensure vm proxy access: %v", err)
	}

	assertPathMode(t, environmentsPath, 0o750)
	assertPathMode(t, dir, 0o750)
	assertPathMode(t, vsockPath, 0o660)

	if os.Geteuid() == 0 {
		assertPathOwner(t, environmentsPath, uid, gid)
		assertPathOwner(t, dir, uid, gid)
		assertPathOwner(t, vsockPath, uid, gid)
	}
}

func TestEnsureVMProxyAccessRequiresVsockSocket(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	missingPath := filepath.Join(dataDir, environmentsDir, "env_missing", vsockSocketName)
	if err := os.MkdirAll(filepath.Dir(missingPath), 0o750); err != nil {
		t.Fatalf("create missing socket directory: %v", err)
	}

	manager := Manager{DataDir: dataDir}

	err := manager.ensureVMProxyAccess(VM{VsockSocketPath: missingPath})
	if err == nil || !strings.Contains(err.Error(), "prepare vsock proxy socket") {
		t.Fatalf("ensure vm proxy access error = %v, want missing vsock socket error", err)
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

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %o, want %o", path, got, want)
	}
}

func assertPathOwner(t *testing.T, path string, wantUID, wantGID int) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat %s returned %T, want *syscall.Stat_t", path, info.Sys())
	}

	if int(stat.Uid) != wantUID || int(stat.Gid) != wantGID {
		t.Fatalf("owner %s = %d:%d, want %d:%d", path, stat.Uid, stat.Gid, wantUID, wantGID)
	}
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

func writeTestPreparedTemplate(t *testing.T, dataDir, templateID string) {
	t.Helper()

	dir := templateDir(dataDir, templateID)
	snapshotDir := filepath.Join(dir, snapshotDirName)

	if err := os.MkdirAll(snapshotDir, 0o750); err != nil {
		t.Fatalf("create prepared template dirs: %v", err)
	}

	files := map[string]string{
		filepath.Join(dir, envRootfsFileName):              "rootfs",
		filepath.Join(dir, envSeedFileName):                "seed",
		filepath.Join(snapshotDir, snapshotConfigFileName): `{"disks":[{"path":"rootfs.img"}],"net":[{}]}`,
		filepath.Join(snapshotDir, snapshotStateFileName):  "{}",
		filepath.Join(snapshotDir, snapshotMemoryFileName): "memory",
	}

	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write prepared template file %s: %v", filepath.Base(path), err)
		}
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}
