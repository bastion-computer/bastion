package cloudhypervisor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
	"github.com/klauspost/compress/zstd"
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

func TestWaitForGuestSSHRetriesUntilAuthenticated(t *testing.T) {
	t.Parallel()

	trueAttempts := 0
	readyAttempts := 0
	manager := Manager{stream: func(_ context.Context, _ io.Writer, name string, args ...string) error {
		if name != "ssh" {
			t.Fatalf("command name = %q, want ssh", name)
		}

		command := args[len(args)-1]
		switch {
		case command == "sh -c 'true'":
			trueAttempts++
			if trueAttempts < 3 {
				return errors.New("ssh failed: exit status 255: root@10.241.0.2: permission denied (publickey)")
			}

			return nil
		case strings.Contains(command, "cloud-init status --wait"):
			readyAttempts++

			return nil
		default:
			t.Fatalf("ssh command = %q, want true or guest readiness probe", command)

			return nil
		}
	}}

	if err := manager.waitForGuestSSHWithInterval(context.Background(), testActionVM(), time.Second, time.Millisecond); err != nil {
		t.Fatalf("wait for guest ssh: %v", err)
	}

	if trueAttempts != 3 || readyAttempts != 1 {
		t.Fatalf("attempts = true:%d ready:%d, want true:3 ready:1", trueAttempts, readyAttempts)
	}
}

func TestRunGuestCommandStopsWhenVMProcessExits(t *testing.T) {
	t.Parallel()

	cmd := startTestCloudHypervisorProcess(t)
	entered := make(chan struct{})
	manager := Manager{stream: func(ctx context.Context, _ io.Writer, name string, _ ...string) error {
		if name != "ssh" {
			return fmt.Errorf("command name = %q, want ssh", name)
		}

		close(entered)
		<-ctx.Done()

		return ctx.Err()
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	vm := testActionVM()
	vm.EnvironmentID = "env_killed"
	vm.VMID = "vm-killed"
	vm.PID = cmd.Process.Pid

	go func() {
		errCh <- manager.runGuestCommand(ctx, vm, "sleep 600", nil)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		cancel()
		t.Fatalf("guest command did not start")
	}

	if err := cmd.Process.Kill(); err != nil {
		cancel()
		t.Fatalf("kill test cloud-hypervisor process: %v", err)
	}

	_ = cmd.Wait()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("guest command error is nil, want VM process exit")
		}

		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("guest command waited for parent context deadline: %v", err)
		}

		if !strings.Contains(err.Error(), "cloud-hypervisor process exited") {
			t.Fatalf("guest command error = %v, want cloud-hypervisor process exit", err)
		}
	case <-time.After(500 * time.Millisecond):
		cancel()
		<-errCh
		t.Fatal("guest command did not stop after VM process exited")
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

func TestManagerExportsAndImportsPreparedTemplateArchive(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sourceID := "tpl_source"
	restoredID := "tpl_restored"
	sourceKey := "source-template"
	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

	writeTestCloudHypervisorAssets(t, dataDir)
	writeTestPreparedTemplate(t, dataDir, sourceID)

	manager := Manager{DataDir: dataDir}

	var archive bytes.Buffer
	if err := manager.ExportTemplate(context.Background(), ExportTemplateRequest{
		Template: Template{ID: sourceID, Key: &sourceKey, Config: config},
		Writer:   &archive,
	}); err != nil {
		t.Fatalf("export template archive: %v", err)
	}

	archiveBytes := archive.Bytes()
	if len(archiveBytes) < 4 || !bytes.HasPrefix(archiveBytes, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		t.Fatalf("archive prefix = % x, want zstd frame magic", archiveBytes[:min(len(archiveBytes), 4)])
	}

	imported, err := manager.ImportTemplate(context.Background(), ImportTemplateRequest{TemplateID: restoredID, Reader: bytes.NewReader(archiveBytes)})
	if err != nil {
		t.Fatalf("import template archive: %v", err)
	}

	if imported.Template.ID != restoredID || imported.Template.Key != nil || !jsonEqual(imported.Template.Config, config) {
		t.Fatalf("imported template = %#v, want restored id/config without key", imported.Template)
	}

	assertPreparedTemplateFiles(t, dataDir, restoredID)
	assertPathMode(t, filepath.Join(templateDir(dataDir, restoredID), envRootfsFileName), 0o400)
	assertPathMode(t, filepath.Join(templateDir(dataDir, restoredID), templateArchiveSSHKeyName), 0o600)

	prepared, err := loadPreparedTemplate(dataDir, restoredID)
	if err != nil {
		t.Fatalf("load imported prepared template: %v", err)
	}

	if prepared.SSHKeyPath == "" {
		t.Fatal("imported prepared template SSHKeyPath is empty")
	}
}

func TestManagerExportsPreparedTemplateArchiveWithoutSnapshotMemory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sourceID := "tpl_source"
	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

	writeTestCloudHypervisorAssets(t, dataDir)
	writeTestPreparedTemplate(t, dataDir, sourceID)

	manager := Manager{DataDir: dataDir}

	var archive bytes.Buffer
	if err := manager.ExportTemplate(context.Background(), ExportTemplateRequest{
		Template: Template{ID: sourceID, Config: config},
		Writer:   &archive,
	}); err != nil {
		t.Fatalf("export template archive: %v", err)
	}

	entries := templateArchiveEntryNames(t, archive.Bytes())
	if entries[path.Join(snapshotDirName, snapshotMemoryFileName)] {
		t.Fatalf("archive unexpectedly contains %s", path.Join(snapshotDirName, snapshotMemoryFileName))
	}
}

func TestManagerImportsPreparedTemplateArchiveWithoutSnapshotMemory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	restoredID := "tpl_restored"
	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

	writeTestCloudHypervisorAssets(t, dataDir)

	var archive bytes.Buffer
	writeZstdTemplateArchive(t, &archive, config, map[string][]byte{
		envRootfsFileName:         []byte("rootfs"),
		envSeedFileName:           []byte("seed"),
		templateArchiveSSHKeyName: []byte("imported-key"),
		templateArchiveSSHPubName: []byte("imported-key-pub"),
	})

	manager := Manager{DataDir: dataDir}

	imported, err := manager.ImportTemplate(context.Background(), ImportTemplateRequest{TemplateID: restoredID, Reader: bytes.NewReader(archive.Bytes())})
	if err != nil {
		t.Fatalf("import disk-only template archive: %v", err)
	}

	if imported.Template.ID != restoredID || !jsonEqual(imported.Template.Config, config) {
		t.Fatalf("imported template = %#v, want restored id/config", imported.Template)
	}

	if _, err := os.Stat(filepath.Join(templateDir(dataDir, restoredID), snapshotDirName, snapshotMemoryFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("imported snapshot memory stat error = %v, want not exist", err)
	}
}

func TestPrepareRestoreWorkspaceUsesImportedTemplateSSHKey(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	templateID := "tpl_imported_key"

	writeTestCloudHypervisorAssets(t, dataDir)
	writeTestPreparedTemplate(t, dataDir, templateID)

	importedKeyPath := filepath.Join(templateDir(dataDir, templateID), templateArchiveSSHKeyName)
	if err := os.WriteFile(importedKeyPath, []byte("imported-key"), 0o600); err != nil {
		t.Fatalf("write imported key: %v", err)
	}

	if err := os.WriteFile(importedKeyPath+".pub", []byte("imported-key-pub"), 0o600); err != nil {
		t.Fatalf("write imported public key: %v", err)
	}

	manager := Manager{DataDir: dataDir, run: func(context.Context, string, ...string) error { return nil }}

	workspace, err := manager.prepareRestoreWorkspace(context.Background(), "env_imported_key", Template{ID: templateID})
	if err != nil {
		t.Fatalf("prepare restore workspace: %v", err)
	}

	if workspace.sshKeyPath != importedKeyPath {
		t.Fatalf("restore workspace ssh key = %q, want imported key %q", workspace.sshKeyPath, importedKeyPath)
	}
}

func TestPrepareRestoreWorkspaceDoesNotRequireTemplateSnapshot(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestCloudHypervisorAssets(t, dataDir)

	templateID := "tpl_disk_only"
	writeDiskOnlyPreparedTemplate(t, dataDir, templateID)

	manager := Manager{DataDir: dataDir, run: func(context.Context, string, ...string) error { return nil }}

	workspace, err := manager.prepareRestoreWorkspace(context.Background(), "env_disk_boot", Template{ID: templateID})
	if err != nil {
		t.Fatalf("prepare restore workspace: %v", err)
	}

	wantSeedPath := filepath.Join(envDir(dataDir, "env_disk_boot"), envSeedFileName)
	if workspace.seedPath != wantSeedPath {
		t.Fatalf("restore workspace seed path = %q, want environment seed %q", workspace.seedPath, wantSeedPath)
	}
}

func TestManagerImportTemplateRejectsGzipArchive(t *testing.T) {
	t.Parallel()

	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

	var archive bytes.Buffer

	writeGzipTemplateArchive(t, &archive, config)

	manager := Manager{DataDir: t.TempDir()}

	_, err := manager.ImportTemplate(context.Background(), ImportTemplateRequest{TemplateID: "tpl_gzip", Reader: bytes.NewReader(archive.Bytes())})
	if !errors.Is(err, ErrInvalidTemplateArchive) {
		t.Fatalf("import gzip archive error = %v, want invalid archive", err)
	}
}

func TestManagerImportTemplateRejectsUnsafeArchivePath(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer

	zstdWriter, err := zstd.NewWriter(&archive)
	if err != nil {
		t.Fatalf("create zstd writer: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	if err := tarWriter.WriteHeader(&tar.Header{Name: "../rootfs.img", Mode: 0o600, Size: 4}); err != nil {
		t.Fatalf("write unsafe header: %v", err)
	}

	if _, err := io.WriteString(tarWriter, "test"); err != nil {
		t.Fatalf("write unsafe file: %v", err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close zstd: %v", err)
	}

	manager := Manager{DataDir: t.TempDir()}
	if _, err := manager.ImportTemplate(context.Background(), ImportTemplateRequest{TemplateID: "tpl_unsafe", Reader: bytes.NewReader(archive.Bytes())}); err == nil {
		t.Fatal("import unsafe archive path error = nil, want error")
	}
}

func TestInstallBaseRecomputesInstalledContentAddress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataDir := t.TempDir()
	srcDir := filepath.Join(dataDir, "source-base")

	writeTestBaseArtifacts(t, srcDir)

	staleAddress, err := basearchive.ContentAddressForFiles(ctx, basearchive.Files(srcDir))
	if err != nil {
		t.Fatalf("compute original base content address: %v", err)
	}

	rootfsPath := filepath.Join(srcDir, basearchive.RootfsName)
	if err := os.Chmod(rootfsPath, 0o600); err != nil {
		t.Fatalf("make source base rootfs writable: %v", err)
	}

	if err := os.WriteFile(rootfsPath, []byte("changed-rootfs"), 0o600); err != nil {
		t.Fatalf("mutate source base rootfs: %v", err)
	}

	metadata, err := installBase(ctx, dataDir, srcDir, basearchive.Metadata{ContentAddress: staleAddress, CreatedAt: "created", UpdatedAt: "created"})
	if err != nil {
		t.Fatalf("install base: %v", err)
	}

	wantAddress, err := basearchive.ContentAddressForFiles(ctx, basearchive.Files(baseDir(dataDir)))
	if err != nil {
		t.Fatalf("compute installed base content address: %v", err)
	}

	if metadata.ContentAddress != wantAddress {
		t.Fatalf("installed content address = %q, want %q", metadata.ContentAddress, wantAddress)
	}

	if metadata.ContentAddress == staleAddress {
		t.Fatalf("installed content address = stale address %q", staleAddress)
	}

	loaded, err := loadBase(dataDir)
	if err != nil {
		t.Fatalf("load installed base: %v", err)
	}

	if loaded.ContentAddress != wantAddress {
		t.Fatalf("loaded content address = %q, want %q", loaded.ContentAddress, wantAddress)
	}

	var archive bytes.Buffer
	if err := basearchive.Write(ctx, &archive, loaded, basearchive.Files(baseDir(dataDir))); err != nil {
		t.Fatalf("export installed base archive: %v", err)
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

	config := buildVMConfig(workspace, networkPlan{}, 1, gibBytes)
	if !config.Disks[0].BackingFiles {
		t.Fatalf("rootfs backing files = false, want true for environment overlay")
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
		{name: "id_rsa.pub", mode: 0o600},
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

func writeDiskOnlyPreparedTemplate(t *testing.T, dataDir, templateID string) {
	t.Helper()

	dir := templateDir(dataDir, templateID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create prepared template dir: %v", err)
	}

	files := map[string]string{
		filepath.Join(dir, envRootfsFileName): "rootfs",
		filepath.Join(dir, envSeedFileName):   "seed",
	}

	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write prepared template file %s: %v", filepath.Base(path), err)
		}
	}
}

func writeTestBaseArtifacts(t *testing.T, dir string) {
	t.Helper()

	for _, file := range basearchive.Files(dir) {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o750); err != nil {
			t.Fatalf("create base artifact dir %s: %v", filepath.Dir(file.Path), err)
		}

		if err := os.WriteFile(file.Path, []byte(file.Name), file.Mode); err != nil {
			t.Fatalf("write base artifact %s: %v", file.Name, err)
		}
	}
}

func writeGzipTemplateArchive(t *testing.T, writer io.Writer, config json.RawMessage) {
	t.Helper()

	gzipWriter := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gzipWriter)

	manifest := templateArchiveManifest{
		Format: templateArchiveFormat,
		Template: Template{
			ID:     "tpl_gzip_source",
			Config: config,
		},
	}

	manifestContents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}

	manifestContents = append(manifestContents, '\n')
	entries := map[string][]byte{
		templateArchiveManifestName: manifestContents,
		envRootfsFileName:           []byte("rootfs"),
		envSeedFileName:             []byte("seed"),
		templateArchiveSSHKeyName:   []byte("ssh-key"),
		templateArchiveSSHPubName:   []byte("ssh-key-pub"),
	}

	for name, contents := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}

		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

func writeZstdTemplateArchive(t *testing.T, writer io.Writer, config json.RawMessage, entries map[string][]byte) {
	t.Helper()

	zstdWriter, err := zstd.NewWriter(writer)
	if err != nil {
		t.Fatalf("create zstd writer: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	manifest := templateArchiveManifest{
		Format: templateArchiveFormat,
		Template: Template{
			ID:     "tpl_source",
			Config: config,
		},
	}

	manifestContents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}

	manifestContents = append(manifestContents, '\n')
	entries[templateArchiveManifestName] = manifestContents

	for name, contents := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}

		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close zstd: %v", err)
	}
}

func templateArchiveEntryNames(t *testing.T, archive []byte) map[string]bool {
	t.Helper()

	zstdReader, err := zstd.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)
	entries := map[string]bool{}

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}

		if err != nil {
			t.Fatalf("read archive: %v", err)
		}

		entries[header.Name] = true
	}
}

func assertPreparedTemplateFiles(t *testing.T, dataDir, templateID string) {
	t.Helper()

	dir := templateDir(dataDir, templateID)
	for _, path := range []string{
		filepath.Join(dir, envRootfsFileName),
		filepath.Join(dir, envSeedFileName),
		filepath.Join(dir, templateArchiveSSHKeyName),
		filepath.Join(dir, templateArchiveSSHPubName),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat imported template file %s: %v", path, err)
		}

		if !info.Mode().IsRegular() {
			t.Fatalf("imported template file %s is not regular", path)
		}
	}
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}

	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}

	return fmt.Sprintf("%#v", leftValue) == fmt.Sprintf("%#v", rightValue)
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
