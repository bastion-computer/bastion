// Package firecracker manages Firecracker system dependencies.
package firecracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/system/command"
	"github.com/bastion-computer/bastion/core/internal/system/dependencies"
	"github.com/bastion-computer/bastion/core/internal/system/host"
	"github.com/bastion-computer/bastion/core/internal/system/utilities"
)

const (
	dependencyName     = "firecracker"
	jailerName         = "jailer"
	linuxOS            = "linux"
	archX8664          = "x86_64"
	archAarch64        = "aarch64"
	utilityUnsquashfs  = "unsquashfs"
	utilitySSHKeygen   = "ssh-keygen"
	utilityMkfsExt4    = "mkfs.ext4"
	utilityE2fsck      = "e2fsck"
	utilityChown       = "chown"
	utilitySudo        = "sudo"
	retainedUtilityMsg = "system utilities installed for Firecracker were not removed"
)

var requiredUtilities = []utilities.Utility{
	{Name: utilityUnsquashfs},
	{Name: utilitySSHKeygen},
	{Name: utilityMkfsExt4},
	{Name: utilityE2fsck},
	{Name: utilityChown},
	{Name: utilitySudo},
}

// UtilityInstaller installs missing host utilities.
type UtilityInstaller interface {
	Install(context.Context, []utilities.Utility) error
}

// AssetDownloader downloads Firecracker assets into a store.
type AssetDownloader interface {
	Download(context.Context, Store, string) (Manifest, error)
}

// RootBuilder builds an ext4 rootfs from downloaded assets.
type RootBuilder interface {
	Build(context.Context, Store, Manifest) (Manifest, error)
}

// Dependency manages the Firecracker system dependency.
type Dependency struct {
	Host             host.Probe
	Utilities        utilities.Registry
	UtilityInstaller UtilityInstaller
	Store            Store
	Downloader       AssetDownloader
	RootFSBuilder    RootBuilder
}

// NewDependency returns a Firecracker system dependency rooted in dataDir.
func NewDependency(dataDir string) Dependency {
	return NewDependencyWithOutput(dataDir, nil, nil)
}

// NewDependencyWithOutput returns a Firecracker system dependency that streams command output to out and errOut.
func NewDependencyWithOutput(dataDir string, out, errOut io.Writer) Dependency {
	probe := host.NewProbe(dataDir)
	runner := command.ExecRunner{Out: out, Err: errOut}

	return Dependency{
		Host: probe,
		Utilities: utilities.Registry{
			Required: requiredUtilities,
			LookPath: probe.LookPath,
		},
		UtilityInstaller: utilities.Installer{Runner: runner, LookPath: probe.LookPath},
		Store:            NewStore(dataDir),
		Downloader:       Downloader{},
		RootFSBuilder:    RootFSBuilder{Runner: runner},
	}
}

// Name returns the Firecracker dependency name.
func (d Dependency) Name() string {
	return dependencyName
}

// ResolveDependencies returns the Firecracker dependency tree.
func (d Dependency) ResolveDependencies(context.Context) dependencies.Node {
	d = d.withDefaults()

	return dependencies.Node{
		Name: dependencyName,
		Children: []dependencies.Node{
			d.hostNode(),
			d.Utilities.Node(),
			d.Store.AssetsNode(),
		},
	}
}

// Add installs Firecracker assets and builds a rootfs.
func (d Dependency) Add(ctx context.Context, opts dependencies.AddOptions) (dependencies.AddResult, error) {
	d = d.withDefaults()

	if !d.hostNode().Available() {
		return dependencies.AddResult{}, errors.New("firecracker host requirements are not satisfied")
	}

	if err := d.ensureUtilities(ctx, opts); err != nil {
		return dependencies.AddResult{}, err
	}

	if err := d.Store.Ensure(); err != nil {
		return dependencies.AddResult{}, err
	}

	manifest, err := d.Downloader.Download(ctx, d.Store, d.Host.Arch)
	if err != nil {
		return dependencies.AddResult{}, err
	}

	manifest, err = d.RootFSBuilder.Build(ctx, d.Store, manifest)
	if err != nil {
		return dependencies.AddResult{}, err
	}

	if err := d.Store.WriteManifest(manifest); err != nil {
		return dependencies.AddResult{}, fmt.Errorf("write firecracker manifest: %w", err)
	}

	return dependencies.AddResult{Path: d.Store.Dir}, nil
}

// Remove removes Firecracker assets from the Bastion data directory.
func (d Dependency) Remove(context.Context) (dependencies.RemoveResult, error) {
	d = d.withDefaults()

	if err := d.Store.Remove(); err != nil {
		return dependencies.RemoveResult{}, err
	}

	return dependencies.RemoveResult{
		Path:  d.Store.Dir,
		Notes: []string{retainedUtilityMsg},
	}, nil
}

func (d Dependency) withDefaults() Dependency {
	d.Host = d.Host.WithDefaults()

	if d.Utilities.LookPath == nil {
		d.Utilities.LookPath = d.Host.LookPath
	}

	if len(d.Utilities.Required) == 0 {
		d.Utilities.Required = requiredUtilities
	}

	if d.UtilityInstaller == nil {
		d.UtilityInstaller = utilities.Installer{Runner: command.ExecRunner{}, LookPath: d.Host.LookPath}
	}

	if d.Store.Dir == "" {
		d.Store = NewStore(d.Host.DataDir)
	}

	d.Store = d.Store.withDefaults()

	if d.Downloader == nil {
		d.Downloader = Downloader{}
	}

	if d.RootFSBuilder == nil {
		d.RootFSBuilder = RootFSBuilder{Runner: command.ExecRunner{}}
	}

	return d
}

func (d Dependency) hostNode() dependencies.Node {
	return dependencies.Node{
		Name: "host",
		Children: []dependencies.Node{
			{Name: linuxOS, OK: d.Host.IsLinux()},
			{Name: "supported architecture: " + d.Host.Arch, OK: d.Host.SupportsArch(archX8664, archAarch64)},
			{Name: host.KVMPath() + " exists", OK: d.Host.KVMExists()},
			{Name: host.KVMPath() + " read/write", OK: d.Host.KVMReadWrite()},
		},
	}
}

func (d Dependency) ensureUtilities(ctx context.Context, opts dependencies.AddOptions) error {
	missing := d.Utilities.Missing()
	if len(missing) == 0 {
		return nil
	}

	confirmed, err := (utilities.Prompt{Yes: opts.WithUtils, In: opts.In, Out: opts.Out}).ConfirmInstall(missing)
	if err != nil {
		return err
	}

	if !confirmed {
		return fmt.Errorf("missing utilities: %s", strings.Join(utilityNames(missing), ", "))
	}

	if err := d.UtilityInstaller.Install(ctx, missing); err != nil {
		return err
	}

	missing = d.Utilities.Missing()
	if len(missing) > 0 {
		return fmt.Errorf("missing utilities after install: %s", strings.Join(utilityNames(missing), ", "))
	}

	return nil
}

func utilityNames(values []utilities.Utility) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}

	return names
}
