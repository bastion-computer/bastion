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

func TestCheckFirecrackerReportsAvailableSystem(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newFirecrackerStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, firecrackerName), 0o755)
	writeTestFile(t, filepath.Join(store.dir, jailerName), 0o755)
	writeTestFile(t, filepath.Join(store.dir, "vmlinux-6.1.155"), 0o600)
	writeTestFile(t, filepath.Join(store.dir, "ubuntu-24.04.squashfs"), 0o600)
	writeTestFile(t, filepath.Join(store.dir, "ubuntu-24.04.ext4"), 0o600)
	writeTestFile(t, filepath.Join(store.dir, "ubuntu-24.04.id_rsa"), 0o600)

	tree := checkFirecracker(testProbe(dataDir, utilityAvailability(firecrackerUtilities...)))
	if !tree.Available() {
		t.Fatalf("tree available = false, want true: %#v", tree)
	}
}

func TestAddFirecrackerInstallsMissingUtilitiesWithFlag(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(
		packageManagerApt,
		utilitySSHKeygen,
		utilityMkfsExt4,
		utilityE2fsck,
		utilityChown,
		utilitySudo,
	)
	runner := &recordRunner{afterRun: func(name string, args []string) {
		if name == packageManagerApt && slices.Contains(args, packageSquashfs) {
			available.add(utilityUnsquashfs)
		}
	}}

	var out bytes.Buffer

	result, err := AddFirecracker(context.Background(), AddFirecrackerOptions{
		DataDir:       dataDir,
		WithUtilities: true,
		Out:           &out,
		Runner:        runner,
		downloader:    fakeDownloader{},
		rootFSBuilder: fakeRootFSBuilder{},
		probe:         testProbe(dataDir, available),
		euid:          func() int { return 0 },
	})
	if err != nil {
		t.Fatalf("add firecracker: %v", err)
	}

	if result.Path != filepath.Join(dataDir, firecrackerName) {
		t.Fatalf("result path = %q, want firecracker store", result.Path)
	}

	if !runner.ran(packageManagerApt, "install", "-y", packageSquashfs) {
		t.Fatalf("runner commands = %#v, want apt-get install squashfs-tools", runner.commands)
	}

	manifest := newFirecrackerStore(dataDir).readManifest()
	if manifest.RootFSExt4 != "ubuntu-24.04.ext4" || manifest.SSHKey != "ubuntu-24.04.id_rsa" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	for _, want := range []string{
		"firecracker: checking host requirements",
		"firecracker: checking required utilities",
		"firecracker: installing missing utilities: unsquashfs",
		"firecracker: creating data directory",
		"firecracker: writing manifest",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("add output missing %q:\n%s", want, out.String())
		}
	}
}

func TestAddFirecrackerPromptsBeforeInstallingUtilities(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	available := utilityAvailability(
		utilitySSHKeygen,
		utilityMkfsExt4,
		utilityE2fsck,
		utilityChown,
		utilitySudo,
	)

	var out bytes.Buffer

	_, err := AddFirecracker(context.Background(), AddFirecrackerOptions{
		DataDir: dataDir,
		In:      strings.NewReader("n\n"),
		Out:     &out,
		Runner:  &recordRunner{},
		probe:   testProbe(dataDir, available),
		euid:    func() int { return 0 },
	})
	if err == nil {
		t.Fatal("add firecracker error = nil, want error")
	}

	if !strings.Contains(out.String(), "install missing utilities? [y/N]") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestRemoveFirecrackerOnlyRemovesFirecrackerData(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newFirecrackerStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, firecrackerName), 0o755)
	writeTestFile(t, filepath.Join(dataDir, "sqlite.db"), 0o600)

	result, err := RemoveFirecracker(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("remove firecracker: %v", err)
	}

	if _, err := os.Stat(store.dir); !os.IsNotExist(err) {
		t.Fatalf("firecracker dir stat error = %v, want not exist", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "sqlite.db")); err != nil {
		t.Fatalf("sqlite db stat: %v", err)
	}

	if len(result.Notes) != 1 || result.Notes[0] != retainedUtilityNote {
		t.Fatalf("remove notes = %#v, want retained utility note", result.Notes)
	}
}

func TestFirecrackerExt4BuilderRunsRootfsCommands(t *testing.T) {
	t.Parallel()

	store := newFirecrackerStore(t.TempDir())
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

	builder := firecrackerExt4Builder{runner: runner, out: &out, size: 1024}

	manifest, err := builder.build(context.Background(), store, firecrackerManifest{RootFSSquashfs: "ubuntu-24.04.squashfs"})
	if err != nil {
		t.Fatalf("build rootfs: %v", err)
	}

	if manifest.RootFSExt4 != "ubuntu-24.04.ext4" || manifest.SSHKey != "ubuntu-24.04.id_rsa" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	for _, name := range []string{utilityUnsquashfs, utilitySSHKeygen, utilitySudo, utilityE2fsck} {
		if !runner.ranName(name) {
			t.Fatalf("runner commands = %#v, want command %q", runner.commands, name)
		}
	}

	if !runner.ranSudo(utilityMkfsExt4) {
		t.Fatalf("runner commands = %#v, want sudo mkfs.ext4", runner.commands)
	}

	for _, want := range []string{
		"firecracker: extracting squashfs rootfs",
		"firecracker: generating SSH key",
		"firecracker: adding SSH key to rootfs",
		"firecracker: setting rootfs ownership",
		"firecracker: creating ext4 rootfs image",
		"firecracker: validating ext4 rootfs image",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("rootfs output missing %q:\n%s", want, out.String())
		}
	}
}

func testProbe(dataDir string, available *utilitySet) firecrackerProbe {
	return firecrackerProbe{
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

func (r *recordRunner) ranSudo(name string) bool {
	for _, command := range r.commands {
		if command.name == utilitySudo && len(command.args) > 0 && command.args[0] == name {
			return true
		}
	}

	return false
}

type fakeDownloader struct{}

func (fakeDownloader) download(_ context.Context, _ firecrackerStore, arch string) (firecrackerManifest, error) {
	return firecrackerManifest{
		Version:        "v1.15.1",
		Architecture:   arch,
		Firecracker:    firecrackerName,
		Jailer:         jailerName,
		Kernel:         "vmlinux-6.1.155",
		RootFSSquashfs: "ubuntu-24.04.squashfs",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

type fakeRootFSBuilder struct{}

func (fakeRootFSBuilder) build(
	_ context.Context,
	_ firecrackerStore,
	manifest firecrackerManifest,
) (firecrackerManifest, error) {
	manifest.RootFSExt4 = "ubuntu-24.04.ext4"
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
