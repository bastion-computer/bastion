package system

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCheckCloudHypervisorReportsAvailableSystem(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newCloudHypervisorStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, cloudHypervisorName), 0o755)
	writeTestFile(t, filepath.Join(store.dir, ubuntuNobleKernelName), 0o600)
	writeTestFile(t, filepath.Join(store.dir, ubuntuNobleInitramfsName), 0o600)
	writeTestFile(t, filepath.Join(store.dir, "ubuntu-24.04.img"), 0o600)
	writeTestFile(t, filepath.Join(store.dir, "ubuntu-24.04.id_rsa"), 0o600)

	tree := checkCloudHypervisor(testProbe(dataDir, utilityAvailability(cloudHypervisorUtilities...)))
	if !tree.Available() {
		t.Fatalf("tree available = false, want true: %#v", tree)
	}
}

func TestAddCloudHypervisorInstallsMissingUtilitiesWithFlag(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(
		packageManagerApt,
		utilitySSHKeygen,
		utilitySSH,
		utilitySCP,
		utilityMkfsVFat,
		utilityMCopy,
		utilityIP,
		utilityIPTables,
		utilitySysctl,
		utilityChown,
		utilitySudo,
	)
	runner := &recordRunner{afterRun: func(name string, args []string) {
		if name == packageManagerApt && slices.Contains(args, packageQEMUUtils) {
			available.add(utilityQEMUImg)
		}
	}}

	var out bytes.Buffer

	result, err := AddCloudHypervisor(context.Background(), AddCloudHypervisorOptions{
		DataDir:       dataDir,
		WithUtilities: true,
		Out:           &out,
		Runner:        runner,
		downloader:    fakeDownloader{},
		imageBuilder:  fakeImageBuilder{},
		probe:         testProbe(dataDir, available),
		euid:          func() int { return 0 },
	})
	if err != nil {
		t.Fatalf("add cloud-hypervisor: %v", err)
	}

	if result.Path != filepath.Join(dataDir, cloudHypervisorName) {
		t.Fatalf("result path = %q, want cloud-hypervisor store", result.Path)
	}

	if !runner.ran(packageManagerApt, "install", "-y", packageQEMUUtils) {
		t.Fatalf("runner commands = %#v, want apt-get install qemu-utils", runner.commands)
	}

	manifest := newCloudHypervisorStore(dataDir).readManifest()
	if manifest.CloudHypervisor != cloudHypervisorName || manifest.Kernel != ubuntuNobleKernelName || manifest.Initramfs != ubuntuNobleInitramfsName || manifest.RootFSImage != "ubuntu-24.04.img" || manifest.SSHKey != "ubuntu-24.04.id_rsa" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	for _, want := range []string{
		"bastion: checking host requirements",
		"bastion: checking required utilities",
		"bastion: installing missing utilities: qemu-img",
		"bastion: creating data directory",
		"bastion: writing manifest",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("add output missing %q:\n%s", want, out.String())
		}
	}
}

func TestAddCloudHypervisorPromptsBeforeInstallingUtilities(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(
		utilitySSHKeygen,
		utilitySSH,
		utilitySCP,
		utilityMkfsVFat,
		utilityMCopy,
		utilityIP,
		utilityIPTables,
		utilitySysctl,
		utilityChown,
		utilitySudo,
	)

	var out bytes.Buffer

	_, err := AddCloudHypervisor(context.Background(), AddCloudHypervisorOptions{
		DataDir: dataDir,
		In:      strings.NewReader("n\n"),
		Out:     &out,
		Runner:  &recordRunner{},
		probe:   testProbe(dataDir, available),
		euid:    func() int { return 0 },
	})
	if err == nil {
		t.Fatal("add cloud-hypervisor error = nil, want error")
	}

	if !strings.Contains(out.String(), "install missing utilities? [y/N]") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestRemoveCloudHypervisorOnlyRemovesCloudHypervisorData(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newCloudHypervisorStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, cloudHypervisorName), 0o755)
	writeTestFile(t, filepath.Join(dataDir, "sqlite.db"), 0o600)

	result, err := RemoveCloudHypervisor(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("remove cloud-hypervisor: %v", err)
	}

	if _, err := os.Stat(store.dir); !os.IsNotExist(err) {
		t.Fatalf("cloud-hypervisor dir stat error = %v, want not exist", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "sqlite.db")); err != nil {
		t.Fatalf("sqlite db stat: %v", err)
	}

	if len(result.Notes) != 1 || result.Notes[0] != retainedUtilityNote {
		t.Fatalf("remove notes = %#v, want retained utility note", result.Notes)
	}
}

func TestCloudHypervisorImageBuilderGeneratesSSHKey(t *testing.T) {
	t.Parallel()

	store := newCloudHypervisorStore(t.TempDir())
	if err := store.ensure(); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	runner := &recordRunner{afterRun: func(name string, args []string) {
		if name != utilitySSHKeygen {
			return
		}

		keyPath := flagValue(args, "-f")
		if keyPath == "" {
			return
		}

		writeTestFile(t, keyPath+".pub", 0o600)
	}}

	var out bytes.Buffer

	builder := cloudHypervisorImageBuilderImpl{runner: runner, out: &out}

	manifest, err := builder.build(context.Background(), store, cloudHypervisorManifest{Kernel: ubuntuNobleKernelName, Initramfs: ubuntuNobleInitramfsName, RootFSImage: "ubuntu-24.04.img"})
	if err != nil {
		t.Fatalf("build image: %v", err)
	}

	if manifest.SSHKey != "ubuntu-24.04.id_rsa" {
		t.Fatalf("manifest SSH key = %q, want ubuntu-24.04.id_rsa", manifest.SSHKey)
	}

	if !runner.ranName(utilitySSHKeygen) {
		t.Fatalf("runner commands = %#v, want ssh-keygen", runner.commands)
	}

	if !strings.Contains(out.String(), "bastion: generating SSH key") {
		t.Fatalf("image output = %q", out.String())
	}
}

func testProbe(dataDir string, available *utilitySet) cloudHypervisorProbe {
	return cloudHypervisorProbe{
		dataDir:   dataDir,
		osName:    linuxOS,
		arch:      archX8664,
		lookPath:  available.lookPath,
		stat:      statWithKVM,
		kvmAccess: func() error { return nil },
	}
}

type utilitySet struct {
	available map[string]bool
}

func utilityAvailability(names ...string) *utilitySet {
	set := &utilitySet{available: make(map[string]bool, len(names))}
	set.add(names...)

	return set
}

func (s *utilitySet) add(names ...string) {
	for _, name := range names {
		s.available[name] = true
	}
}

func (s *utilitySet) lookPath(name string) (string, error) {
	if s.available[name] {
		return filepath.Join("/usr/bin", name), nil
	}

	return "", errors.New("not found")
}

type recordRunner struct {
	commands []recordedCommand
	afterRun func(string, []string)
}

type recordedCommand struct {
	name string
	args []string
}

func (r *recordRunner) Run(_ context.Context, name string, args ...string) error {
	r.commands = append(r.commands, recordedCommand{name: name, args: append([]string(nil), args...)})

	if r.afterRun != nil {
		r.afterRun(name, args)
	}

	return nil
}

func (r *recordRunner) ran(name string, args ...string) bool {
	for _, command := range r.commands {
		if command.name == name && slices.Equal(command.args, args) {
			return true
		}
	}

	return false
}

func (r *recordRunner) ranName(name string) bool {
	for _, command := range r.commands {
		if command.name == name {
			return true
		}
	}

	return false
}

type fakeDownloader struct{}

func (fakeDownloader) download(_ context.Context, _ cloudHypervisorStore, arch string) (cloudHypervisorManifest, error) {
	return cloudHypervisorManifest{
		Version:         "v52.0",
		Architecture:    arch,
		CloudHypervisor: cloudHypervisorName,
		Kernel:          ubuntuNobleKernelName,
		Initramfs:       ubuntuNobleInitramfsName,
		RootFSImage:     "ubuntu-24.04.img",
		RootFSImageType: "Qcow2",
		CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

type fakeImageBuilder struct{}

func (fakeImageBuilder) build(
	_ context.Context,
	_ cloudHypervisorStore,
	manifest cloudHypervisorManifest,
) (cloudHypervisorManifest, error) {
	manifest.SSHKey = "ubuntu-24.04.id_rsa"

	return manifest, nil
}

func statWithKVM(path string) (os.FileInfo, error) {
	if path == kvmPath {
		return testFileInfo{name: "kvm", mode: 0o660}, nil
	}

	return os.Stat(path)
}

type testFileInfo struct {
	name string
	mode os.FileMode
}

func (i testFileInfo) Name() string       { return i.name }
func (i testFileInfo) Size() int64        { return 0 }
func (i testFileInfo) Mode() os.FileMode  { return i.mode }
func (i testFileInfo) ModTime() time.Time { return time.Time{} }
func (i testFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i testFileInfo) Sys() any           { return nil }

func writeTestFile(tb testing.TB, path string, mode os.FileMode) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatalf("create parent directory: %v", err)
	}

	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		tb.Fatalf("write test file: %v", err)
	}

	if err := os.Chmod(path, mode); err != nil {
		tb.Fatalf("chmod test file: %v", err)
	}
}

func flagValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}
