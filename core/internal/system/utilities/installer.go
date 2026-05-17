package utilities

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/system/command"
)

const (
	packageManagerApt  = "apt-get"
	packageManagerDNF  = "dnf"
	packageManagerYUM  = "yum"
	packageManagerArch = "pacman"
	utilitySudo        = "sudo"
	packageSquashfs    = "squashfs-tools"
	packageOpenSSH     = "openssh"
	packageOpenSSHDeb  = "openssh-client"
	packageOpenSSHRPM  = "openssh-clients"
	packageE2fsprogs   = "e2fsprogs"
	packageCoreutils   = "coreutils"
	utilityUnsquashfs  = "unsquashfs"
	utilitySSHKeygen   = "ssh-keygen"
	utilityMkfsExt4    = "mkfs.ext4"
	utilityE2fsck      = "e2fsck"
	utilityChown       = "chown"
)

var utilityPackages = map[string]map[string]string{
	packageManagerApt: {
		utilityUnsquashfs: packageSquashfs,
		utilitySSHKeygen:  packageOpenSSHDeb,
		utilityMkfsExt4:   packageE2fsprogs,
		utilityE2fsck:     packageE2fsprogs,
		utilityChown:      packageCoreutils,
		utilitySudo:       utilitySudo,
	},
	packageManagerDNF: {
		utilityUnsquashfs: packageSquashfs,
		utilitySSHKeygen:  packageOpenSSHRPM,
		utilityMkfsExt4:   packageE2fsprogs,
		utilityE2fsck:     packageE2fsprogs,
		utilityChown:      packageCoreutils,
		utilitySudo:       utilitySudo,
	},
	packageManagerYUM: {
		utilityUnsquashfs: packageSquashfs,
		utilitySSHKeygen:  packageOpenSSHRPM,
		utilityMkfsExt4:   packageE2fsprogs,
		utilityE2fsck:     packageE2fsprogs,
		utilityChown:      packageCoreutils,
		utilitySudo:       utilitySudo,
	},
	packageManagerArch: {
		utilityUnsquashfs: packageSquashfs,
		utilitySSHKeygen:  packageOpenSSH,
		utilityMkfsExt4:   packageE2fsprogs,
		utilityE2fsck:     packageE2fsprogs,
		utilityChown:      packageCoreutils,
		utilitySudo:       utilitySudo,
	},
}

// Installer installs missing utilities through a supported package manager.
type Installer struct {
	Runner   command.Runner
	LookPath func(string) (string, error)
	EUID     func() int
}

// Install installs missing utilities with a supported package manager.
func (i Installer) Install(ctx context.Context, missing []Utility) error {
	i = i.withDefaults()

	manager, err := i.packageManager()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.Join(names(missing), ", "))
	}

	packages, err := packagesForUtilities(manager, missing)
	if err != nil {
		return err
	}

	return i.installPackages(ctx, manager, packages)
}

func (i Installer) withDefaults() Installer {
	if i.Runner == nil {
		i.Runner = command.ExecRunner{}
	}

	if i.LookPath == nil {
		i.LookPath = commandLookPath
	}

	if i.EUID == nil {
		i.EUID = os.Geteuid
	}

	return i
}

func (i Installer) packageManager() (string, error) {
	for _, candidate := range []string{packageManagerApt, packageManagerDNF, packageManagerYUM, packageManagerArch} {
		if _, err := i.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("unsupported package manager; install missing utilities manually")
}

func packagesForUtilities(manager string, utilities []Utility) ([]string, error) {
	packageNames, ok := utilityPackages[manager]
	if !ok {
		return nil, fmt.Errorf("unsupported package manager: %s", manager)
	}

	seen := make(map[string]struct{})
	packages := make([]string, 0, len(utilities))

	for _, utility := range utilities {
		name, ok := packageNames[utility.Name]
		if !ok {
			return nil, fmt.Errorf("unsupported utility: %s", utility.Name)
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		packages = append(packages, name)
	}

	return packages, nil
}

func (i Installer) installPackages(ctx context.Context, manager string, packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	switch manager {
	case packageManagerApt:
		if err := i.runPrivileged(ctx, packageManagerApt, "update"); err != nil {
			return err
		}

		return i.runPrivileged(ctx, packageManagerApt, append([]string{"install", "-y"}, packages...)...)
	case packageManagerDNF, packageManagerYUM:
		return i.runPrivileged(ctx, manager, append([]string{"install", "-y"}, packages...)...)
	case packageManagerArch:
		return i.runPrivileged(ctx, packageManagerArch, append([]string{"-Sy", "--needed", "--noconfirm"}, packages...)...)
	default:
		return fmt.Errorf("unsupported package manager: %s", manager)
	}
}

func (i Installer) runPrivileged(ctx context.Context, name string, args ...string) error {
	if i.EUID() == 0 {
		return i.Runner.Run(ctx, name, args...)
	}

	if _, err := i.LookPath(utilitySudo); err != nil {
		return errors.New("sudo is required to install missing utilities")
	}

	return i.Runner.Run(ctx, utilitySudo, append([]string{name}, args...)...)
}

func commandLookPath(name string) (string, error) {
	return exec.LookPath(name)
}
