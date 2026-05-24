package cloudhypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
