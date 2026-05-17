package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
)

const (
	bastionName         = "bastion"
	firecrackerName     = "firecracker"
	jailerName          = "jailer"
	linuxOS             = "linux"
	archX8664           = "x86_64"
	archAarch64         = "aarch64"
	kvmPath             = "/dev/kvm"
	retainedUtilityNote = "system utilities installed for Firecracker were not removed"
)

// Result describes a completed system dependency action.
type Result struct {
	Path  string
	Notes []string
}

// AddFirecrackerOptions configures Firecracker system setup.
type AddFirecrackerOptions struct {
	DataDir       string
	WithUtilities bool
	In            io.Reader
	Out           io.Writer
	Runner        Runner

	downloader    firecrackerDownloader
	rootFSBuilder firecrackerRootFSBuilder
	probe         firecrackerProbe
	euid          func() int
}

type firecrackerDownloader interface {
	download(context.Context, firecrackerStore, string) (firecrackerManifest, error)
}

type firecrackerRootFSBuilder interface {
	build(context.Context, firecrackerStore, firecrackerManifest) (firecrackerManifest, error)
}

type firecrackerProbe struct {
	dataDir   string
	osName    string
	arch      string
	lookPath  func(string) (string, error)
	stat      func(string) (os.FileInfo, error)
	kvmAccess func() error
}

// Check returns the full Bastion system dependency tree.
func Check(_ context.Context, dataDir string) Node {
	return Node{
		Name:     bastionName,
		Children: []Node{checkFirecracker(newFirecrackerProbe(dataDir))},
	}
}

// AddFirecracker installs Firecracker host assets under the Bastion data directory.
func AddFirecracker(ctx context.Context, opts AddFirecrackerOptions) (Result, error) {
	opts = opts.withDefaults()
	if opts.DataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	if !firecrackerHostNode(opts.probe).Available() {
		return Result{}, errors.New("firecracker host requirements are not satisfied")
	}

	if err := ensureFirecrackerUtilities(ctx, opts); err != nil {
		return Result{}, err
	}

	store := newFirecrackerStore(opts.DataDir)
	if err := store.ensure(); err != nil {
		return Result{}, err
	}

	manifest, err := opts.downloader.download(ctx, store, opts.probe.arch)
	if err != nil {
		return Result{}, err
	}

	manifest, err = opts.rootFSBuilder.build(ctx, store, manifest)
	if err != nil {
		return Result{}, err
	}

	if err := store.writeManifest(manifest); err != nil {
		return Result{}, fmt.Errorf("write firecracker manifest: %w", err)
	}

	return Result{Path: store.dir}, nil
}

// RemoveFirecracker removes Firecracker assets from the Bastion data directory.
func RemoveFirecracker(_ context.Context, dataDir string) (Result, error) {
	if dataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	store := newFirecrackerStore(dataDir)
	if err := store.remove(); err != nil {
		return Result{}, err
	}

	return Result{Path: store.dir, Notes: []string{retainedUtilityNote}}, nil
}

func (o AddFirecrackerOptions) withDefaults() AddFirecrackerOptions {
	if o.Runner == nil {
		o.Runner = NewExecRunner(nil, nil)
	}

	if o.probe.dataDir == "" {
		o.probe.dataDir = o.DataDir
	}

	o.probe = o.probe.withDefaults()
	o.DataDir = o.probe.dataDir

	if o.euid == nil {
		o.euid = os.Geteuid
	}

	if o.downloader == nil {
		o.downloader = firecrackerHTTPDownloader{}
	}

	if o.rootFSBuilder == nil {
		o.rootFSBuilder = firecrackerExt4Builder{runner: o.Runner}
	}

	return o
}

func checkFirecracker(probe firecrackerProbe) Node {
	probe = probe.withDefaults()
	store := newFirecrackerStore(probe.dataDir)
	store.stat = probe.stat

	return Node{
		Name: firecrackerName,
		Children: []Node{
			firecrackerHostNode(probe),
			firecrackerUtilitiesNode(probe.lookPath),
			store.assetsNode(),
		},
	}
}

func firecrackerHostNode(probe firecrackerProbe) Node {
	probe = probe.withDefaults()

	return Node{
		Name: "host",
		Children: []Node{
			{Name: linuxOS, OK: probe.osName == linuxOS},
			{Name: "supported architecture: " + probe.arch, OK: slices.Contains([]string{archX8664, archAarch64}, probe.arch)},
			{Name: kvmPath + " exists", OK: probe.kvmExists()},
			{Name: kvmPath + " read/write", OK: probe.kvmReadWrite()},
		},
	}
}

func newFirecrackerProbe(dataDir string) firecrackerProbe {
	return firecrackerProbe{dataDir: dataDir}.withDefaults()
}

func (p firecrackerProbe) withDefaults() firecrackerProbe {
	if p.osName == "" {
		p.osName = runtime.GOOS
	}

	if p.arch == "" {
		p.arch = firecrackerArch(runtime.GOARCH)
	}

	if p.lookPath == nil {
		p.lookPath = exec.LookPath
	}

	if p.stat == nil {
		p.stat = os.Stat
	}

	if p.kvmAccess == nil {
		p.kvmAccess = defaultKVMAccess
	}

	return p
}

func (p firecrackerProbe) kvmExists() bool {
	_, err := p.withDefaults().stat(kvmPath)

	return err == nil
}

func (p firecrackerProbe) kvmReadWrite() bool {
	return p.withDefaults().kvmAccess() == nil
}

func defaultKVMAccess() error {
	file, err := os.OpenFile(kvmPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}

	return file.Close()
}

func firecrackerArch(goarch string) string {
	switch goarch {
	case "amd64":
		return archX8664
	case "arm64":
		return archAarch64
	default:
		return goarch
	}
}
