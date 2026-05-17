package firecracker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/system/dependencies"
	"github.com/bastion-computer/bastion/core/internal/system/host"
	"github.com/bastion-computer/bastion/core/internal/system/utilities"
)

func TestDependencyResolveDependenciesReportsAvailableSystem(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := NewStore(dataDir)
	writeTestFile(t, filepath.Join(store.Dir, dependencyName), 0o755)
	writeTestFile(t, filepath.Join(store.Dir, jailerName), 0o755)
	writeTestFile(t, filepath.Join(store.Dir, "vmlinux-6.1.155"), 0o640)
	writeTestFile(t, filepath.Join(store.Dir, "ubuntu-24.04.squashfs"), 0o640)
	writeTestFile(t, filepath.Join(store.Dir, "ubuntu-24.04.ext4"), 0o640)
	writeTestFile(t, filepath.Join(store.Dir, "ubuntu-24.04.id_rsa"), 0o600)

	dep := testDependency(dataDir, utilityAvailability(utilityNames(requiredUtilities)...))
	tree := dep.ResolveDependencies(context.Background())

	if !tree.Available() {
		t.Fatalf("tree available = false, want true: %#v", tree)
	}
}

func TestDependencyAddInstallsMissingUtilitiesWithYes(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(utilitySSHKeygen, utilityMkfsExt4, utilityE2fsck, utilityChown, utilitySudo)
	installer := &fakeInstaller{available: available}
	dep := testDependency(dataDir, available)
	dep.UtilityInstaller = installer
	dep.Downloader = fakeDownloader{}
	dep.RootFSBuilder = fakeRootBuilder{}

	var out bytes.Buffer

	result, err := dep.Add(context.Background(), addOptions(true, &out))
	if err != nil {
		t.Fatalf("add firecracker: %v", err)
	}

	if result.Path != filepath.Join(dataDir, dependencyName) {
		t.Fatalf("result path = %q, want firecracker store", result.Path)
	}

	if len(installer.installed) != 1 || installer.installed[0].Name != utilityUnsquashfs {
		t.Fatalf("installed utilities = %#v, want unsquashfs", installer.installed)
	}

	manifest := NewStore(dataDir).ReadManifest()
	if manifest.RootFSExt4 != "ubuntu-24.04.ext4" || manifest.SSHKey != "ubuntu-24.04.id_rsa" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestDependencyAddPromptsBeforeInstallingUtilities(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(utilitySSHKeygen, utilityMkfsExt4, utilityE2fsck, utilityChown, utilitySudo)
	dep := testDependency(dataDir, available)
	dep.UtilityInstaller = &fakeInstaller{available: available}
	dep.Downloader = fakeDownloader{}
	dep.RootFSBuilder = fakeRootBuilder{}

	var out bytes.Buffer

	_, err := dep.Add(context.Background(), addOptions(false, &out))
	if err == nil {
		t.Fatal("add firecracker error = nil, want error")
	}

	if !strings.Contains(out.String(), "install missing utilities? [y/N]") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestDependencyRemoveOnlyRemovesFirecrackerData(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := NewStore(dataDir)
	writeTestFile(t, filepath.Join(store.Dir, dependencyName), 0o755)
	writeTestFile(t, filepath.Join(dataDir, "sqlite.db"), 0o640)

	dep := testDependency(dataDir, utilityAvailability(utilityNames(requiredUtilities)...))

	result, err := dep.Remove(context.Background())
	if err != nil {
		t.Fatalf("remove firecracker: %v", err)
	}

	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Fatalf("firecracker dir stat error = %v, want not exist", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "sqlite.db")); err != nil {
		t.Fatalf("sqlite db stat: %v", err)
	}

	if len(result.Notes) != 1 || result.Notes[0] != retainedUtilityMsg {
		t.Fatalf("remove notes = %#v, want retained utility note", result.Notes)
	}
}

func addOptions(yes bool, out *bytes.Buffer) dependencies.AddOptions {
	return dependencies.AddOptions{Yes: yes, In: strings.NewReader("n\n"), Out: out}
}

func testDependency(dataDir string, available *utilitySet) Dependency {
	probe := host.Probe{
		DataDir:        dataDir,
		OS:             linuxOS,
		Arch:           archX8664,
		LookPath:       available.lookPath,
		Stat:           statWithKVM,
		CheckKVMAccess: func() error { return nil },
	}

	return Dependency{
		Host: probe,
		Utilities: utilities.Registry{
			Required: requiredUtilities,
			LookPath: available.lookPath,
		},
		Store: NewStore(dataDir),
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

type fakeInstaller struct {
	available *utilitySet
	installed []utilities.Utility
}

func (i *fakeInstaller) Install(_ context.Context, missing []utilities.Utility) error {
	i.installed = append(i.installed, missing...)
	i.available.add(utilityNames(missing)...)

	return nil
}

type fakeDownloader struct{}

func (fakeDownloader) Download(_ context.Context, _ Store, arch string) (Manifest, error) {
	return Manifest{
		Version:        "v1.15.1",
		Architecture:   arch,
		Firecracker:    dependencyName,
		Jailer:         jailerName,
		Kernel:         "vmlinux-6.1.155",
		RootFSSquashfs: "ubuntu-24.04.squashfs",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

type fakeRootBuilder struct{}

func (fakeRootBuilder) Build(_ context.Context, _ Store, manifest Manifest) (Manifest, error) {
	manifest.RootFSExt4 = "ubuntu-24.04.ext4"
	manifest.SSHKey = "ubuntu-24.04.id_rsa"

	return manifest, nil
}

func statWithKVM(path string) (os.FileInfo, error) {
	if path == host.KVMPath() {
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

type testWriter interface {
	Helper()
	Fatalf(string, ...any)
}

func writeTestFile(t testWriter, path string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create parent directory: %v", err)
	}

	if err := os.WriteFile(path, []byte("test"), mode); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
