package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

// AddOpenCodeOptions configures OpenCode system asset setup.
type AddOpenCodeOptions struct {
	DataDir string
	Out     io.Writer

	downloader openCodeDownloader
	probe      openCodeProbe
}

type openCodeDownloader interface {
	download(context.Context, openCodeStore, string) (opencodeasset.Manifest, error)
}

type openCodeProbe struct {
	dataDir string
	arch    string
	stat    func(string) (os.FileInfo, error)
}

// AddOpenCode installs the pinned OpenCode binary under the Bastion data directory.
func AddOpenCode(ctx context.Context, opts AddOpenCodeOptions) (Result, error) {
	opts = opts.withDefaults()
	if opts.DataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	store := newOpenCodeStore(opts.DataDir)
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

	if err := logCloudHypervisorProgress(opts.Out, "writing OpenCode manifest"); err != nil {
		return Result{}, err
	}

	if err := store.writeManifest(manifest); err != nil {
		return Result{}, fmt.Errorf("write opencode manifest: %w", err)
	}

	return Result{Path: store.dir}, nil
}

// RemoveOpenCode removes OpenCode assets from the Bastion data directory.
func RemoveOpenCode(_ context.Context, dataDir string) (Result, error) {
	if dataDir == "" {
		return Result{}, errors.New("data dir is required")
	}

	store := newOpenCodeStore(dataDir)
	if err := store.remove(); err != nil {
		return Result{}, err
	}

	return Result{Path: store.dir}, nil
}

func (o AddOpenCodeOptions) withDefaults() AddOpenCodeOptions {
	if o.probe.dataDir == "" {
		o.probe.dataDir = o.DataDir
	}

	o.probe = o.probe.withDefaults()
	o.DataDir = o.probe.dataDir

	if o.downloader == nil {
		o.downloader = openCodeHTTPDownloader{out: o.Out}
	}

	return o
}

func checkOpenCode(probe openCodeProbe) Node {
	probe = probe.withDefaults()
	store := newOpenCodeStore(probe.dataDir)
	store.stat = probe.stat

	return Node{
		Name:     opencodeasset.BinaryName,
		Children: []Node{store.assetsNode()},
	}
}

func newOpenCodeProbe(dataDir string) openCodeProbe {
	return openCodeProbe{dataDir: dataDir}.withDefaults()
}

func (p openCodeProbe) withDefaults() openCodeProbe {
	if p.arch == "" {
		p.arch = cloudHypervisorArch(runtime.GOARCH)
	}

	if p.stat == nil {
		p.stat = os.Stat
	}

	return p
}
