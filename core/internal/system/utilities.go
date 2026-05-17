package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	packageManagerApt  = "apt-get"
	packageManagerDNF  = "dnf"
	packageManagerYUM  = "yum"
	packageManagerArch = "pacman"
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
	utilitySudo        = "sudo"
)

var firecrackerUtilities = []string{
	utilityUnsquashfs,
	utilitySSHKeygen,
	utilityMkfsExt4,
	utilityE2fsck,
	utilityChown,
	utilitySudo,
}

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

func firecrackerUtilitiesNode(lookPath func(string) (string, error)) Node {
	children := make([]Node, 0, len(firecrackerUtilities))

	for _, utility := range firecrackerUtilities {
		_, err := lookPath(utility)
		children = append(children, Node{Name: utility, OK: err == nil})
	}

	return Node{Name: "utilities", Children: children}
}

func ensureFirecrackerUtilities(ctx context.Context, opts AddFirecrackerOptions) error {
	missing := missingFirecrackerUtilities(opts.probe.lookPath)
	if len(missing) == 0 {
		return nil
	}

	confirmed, err := confirmInstallUtilities(opts.WithUtilities, opts.In, opts.Out, missing)
	if err != nil {
		return err
	}

	if !confirmed {
		return fmt.Errorf("missing utilities: %s", strings.Join(missing, ", "))
	}

	if err := installUtilities(ctx, opts.Runner, opts.probe.lookPath, opts.euid, missing); err != nil {
		return err
	}

	missing = missingFirecrackerUtilities(opts.probe.lookPath)
	if len(missing) > 0 {
		return fmt.Errorf("missing utilities after install: %s", strings.Join(missing, ", "))
	}

	return nil
}

func missingFirecrackerUtilities(lookPath func(string) (string, error)) []string {
	missing := make([]string, 0)

	for _, utility := range firecrackerUtilities {
		if _, err := lookPath(utility); err != nil {
			missing = append(missing, utility)
		}
	}

	return missing
}

func confirmInstallUtilities(withUtilities bool, in io.Reader, out io.Writer, missing []string) (bool, error) {
	if withUtilities {
		return true, nil
	}

	if in == nil {
		in = os.Stdin
	}

	if out == nil {
		out = io.Discard
	}

	if _, err := fmt.Fprintf(out, "missing utilities: %s\ninstall missing utilities? [y/N] ", strings.Join(missing, ", ")); err != nil {
		return false, err
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	answer := strings.ToLower(strings.TrimSpace(line))

	return answer == "y" || answer == "yes", nil
}

func installUtilities(
	ctx context.Context,
	runner Runner,
	lookPath func(string) (string, error),
	euid func() int,
	missing []string,
) error {
	manager, err := packageManager(lookPath)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.Join(missing, ", "))
	}

	packages, err := packagesForUtilities(manager, missing)
	if err != nil {
		return err
	}

	return installPackages(ctx, runner, lookPath, euid, manager, packages)
}

func packageManager(lookPath func(string) (string, error)) (string, error) {
	for _, candidate := range []string{packageManagerApt, packageManagerDNF, packageManagerYUM, packageManagerArch} {
		if _, err := lookPath(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("unsupported package manager; install missing utilities manually")
}

func packagesForUtilities(manager string, utilities []string) ([]string, error) {
	packageNames, ok := utilityPackages[manager]
	if !ok {
		return nil, fmt.Errorf("unsupported package manager: %s", manager)
	}

	seen := make(map[string]struct{})
	packages := make([]string, 0, len(utilities))

	for _, utility := range utilities {
		name, ok := packageNames[utility]
		if !ok {
			return nil, fmt.Errorf("unsupported utility: %s", utility)
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		packages = append(packages, name)
	}

	return packages, nil
}

func installPackages(
	ctx context.Context,
	runner Runner,
	lookPath func(string) (string, error),
	euid func() int,
	manager string,
	packages []string,
) error {
	if len(packages) == 0 {
		return nil
	}

	switch manager {
	case packageManagerApt:
		if err := runPrivileged(ctx, runner, lookPath, euid, packageManagerApt, "update"); err != nil {
			return err
		}

		return runPrivileged(ctx, runner, lookPath, euid, packageManagerApt, append([]string{"install", "-y"}, packages...)...)
	case packageManagerDNF, packageManagerYUM:
		return runPrivileged(ctx, runner, lookPath, euid, manager, append([]string{"install", "-y"}, packages...)...)
	case packageManagerArch:
		return runPrivileged(ctx, runner, lookPath, euid, packageManagerArch, append([]string{"-Sy", "--needed", "--noconfirm"}, packages...)...)
	default:
		return fmt.Errorf("unsupported package manager: %s", manager)
	}
}

func runPrivileged(
	ctx context.Context,
	runner Runner,
	lookPath func(string) (string, error),
	euid func() int,
	name string,
	args ...string,
) error {
	if euid() == 0 {
		return runner.Run(ctx, name, args...)
	}

	if _, err := lookPath(utilitySudo); err != nil {
		return errors.New("sudo is required to install missing utilities")
	}

	return runner.Run(ctx, utilitySudo, append([]string{name}, args...)...)
}
