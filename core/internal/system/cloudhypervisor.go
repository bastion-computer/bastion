package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

const (
	bastionName         = "bastion"
	cloudHypervisorName = "cloud-hypervisor"
	linuxOS             = "linux"
	archX8664           = "x86_64"
	kvmPath             = "/dev/kvm"
	vhostVsockPath      = "/dev/vhost-vsock"
	retainedUtilityNote = "system utilities installed for Cloud Hypervisor were not removed"
)

// Result describes a completed system dependency action.
type Result struct {
	Path  string
	Notes []string
}

// AddCloudHypervisorOptions configures Cloud Hypervisor system setup.
type AddCloudHypervisorOptions struct {
	DataDir       string
	WithUtilities bool
	In            io.Reader
	Out           io.Writer
	Runner        Runner

	downloader   cloudHypervisorDownloader
	imageBuilder cloudHypervisorImageBuilder
	probe        cloudHypervisorProbe
	euid         func() int
}

type cloudHypervisorDownloader interface {
	download(context.Context, cloudHypervisorStore, string) (cloudHypervisorManifest, error)
}

type cloudHypervisorImageBuilder interface {
	build(context.Context, cloudHypervisorStore, cloudHypervisorManifest) (cloudHypervisorManifest, error)
}

type cloudHypervisorProbe struct {
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
		Children: []Node{checkCloudHypervisor(newCloudHypervisorProbe(dataDir)), checkOpenCode(newOpenCodeProbe(dataDir))},
	}
}

// AddCloudHypervisor installs Cloud Hypervisor host assets under the Bastion data directory.
func AddCloudHypervisor(ctx context.Context, opts AddCloudHypervisorOptions) (Result, error) {
	opts = opts.withDefaults()
	if opts.DataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	if err := logCloudHypervisorProgress(opts.Out, "checking host requirements"); err != nil {
		return Result{}, err
	}

	if !cloudHypervisorHostNode(opts.probe).Available() {
		return Result{}, errors.New("cloud-hypervisor host requirements are not satisfied")
	}

	if err := ensureCloudHypervisorUtilities(ctx, opts); err != nil {
		return Result{}, err
	}

	store := newCloudHypervisorStore(opts.DataDir)
	if err := logCloudHypervisorProgress(opts.Out, "creating data directory %s", store.dir); err != nil {
		return Result{}, err
	}

	if err := store.ensure(); err != nil {
		return Result{}, err
	}

	manifest, err := opts.downloader.download(ctx, store, opts.probe.arch)
	if err != nil {
		return Result{}, err
	}

	manifest, err = opts.imageBuilder.build(ctx, store, manifest)
	if err != nil {
		return Result{}, err
	}

	if err := logCloudHypervisorProgress(opts.Out, "writing manifest"); err != nil {
		return Result{}, err
	}

	if err := store.writeManifest(manifest); err != nil {
		return Result{}, fmt.Errorf("write cloud-hypervisor manifest: %w", err)
	}

	return Result{Path: store.dir}, nil
}

// RemoveCloudHypervisor removes Cloud Hypervisor assets from the Bastion data directory.
func RemoveCloudHypervisor(_ context.Context, dataDir string) (Result, error) {
	if dataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	store := newCloudHypervisorStore(dataDir)
	if err := store.remove(); err != nil {
		return Result{}, err
	}

	return Result{Path: store.dir, Notes: []string{retainedUtilityNote}}, nil
}

func (o AddCloudHypervisorOptions) withDefaults() AddCloudHypervisorOptions {
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
		o.downloader = cloudHypervisorHTTPDownloader{out: o.Out}
	}

	if o.imageBuilder == nil {
		o.imageBuilder = cloudHypervisorImageBuilderImpl{runner: o.Runner, out: o.Out}
	}

	return o
}

func checkCloudHypervisor(probe cloudHypervisorProbe) Node {
	probe = probe.withDefaults()
	store := newCloudHypervisorStore(probe.dataDir)
	store.stat = probe.stat

	return Node{
		Name: cloudHypervisorName,
		Children: []Node{
			cloudHypervisorHostNode(probe),
			cloudHypervisorUtilitiesNode(probe.lookPath),
			store.assetsNode(),
		},
	}
}

func cloudHypervisorHostNode(probe cloudHypervisorProbe) Node {
	probe = probe.withDefaults()

	return Node{
		Name: "host",
		Children: []Node{
			{Name: linuxOS, OK: probe.osName == linuxOS},
			{Name: "supported architecture: " + probe.arch, OK: probe.arch == archX8664},
			{Name: kvmPath + " exists", OK: probe.kvmExists()},
			{Name: kvmPath + " read/write", OK: probe.kvmReadWrite()},
			{Name: vhostVsockPath + " exists", OK: probe.vhostVsockExists()},
		},
	}
}

func newCloudHypervisorProbe(dataDir string) cloudHypervisorProbe {
	return cloudHypervisorProbe{dataDir: dataDir}.withDefaults()
}

func (p cloudHypervisorProbe) withDefaults() cloudHypervisorProbe {
	if p.osName == "" {
		p.osName = runtime.GOOS
	}

	if p.arch == "" {
		p.arch = cloudHypervisorArch(runtime.GOARCH)
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

func (p cloudHypervisorProbe) kvmExists() bool {
	_, err := p.withDefaults().stat(kvmPath)

	return err == nil
}

func (p cloudHypervisorProbe) kvmReadWrite() bool {
	return p.withDefaults().kvmAccess() == nil
}

func (p cloudHypervisorProbe) vhostVsockExists() bool {
	_, err := p.withDefaults().stat(vhostVsockPath)

	return err == nil
}

func defaultKVMAccess() error {
	file, err := os.OpenFile(kvmPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}

	return file.Close()
}

func cloudHypervisorArch(goarch string) string {
	switch goarch {
	case "amd64":
		return archX8664
	default:
		return goarch
	}
}
