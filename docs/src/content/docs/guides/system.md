---
title: System Setup
description: Host requirements and Cloud Hypervisor asset management.
---

Bastion launches environments with Cloud Hypervisor. The `bastion system`
commands check and install the host-side dependencies needed by that runtime.

## Host Requirements

The current runtime expects:

| Requirement       | Details                                    |
| ----------------- | ------------------------------------------ |
| Operating system  | Linux                                      |
| Architecture      | x86_64                                     |
| Virtualization    | `/dev/kvm` exists and is readable/writable |
| Privileged daemon | `bastiond` runs as root                    |

On cloud hosts, the instance must support KVM. If Bastion is running inside a
VM, nested virtualization must be enabled.

## Required Utilities

Bastion checks for these host utilities:

| Utility      | Purpose                                                         |
| ------------ | --------------------------------------------------------------- |
| `ssh-keygen` | Generate the guest SSH key used by environments.                |
| `ssh`        | Run template init commands inside the guest.                    |
| `scp`        | Copy action packages into the guest.                            |
| `qemu-img`   | Prepare root file system images.                                |
| `mkfs.vfat`  | Build cloud-init seed media.                                    |
| `mcopy`      | Write cloud-init files into seed media.                         |
| `ip`         | Create and configure TAP interfaces.                            |
| `iptables`   | Configure forwarding and NAT.                                   |
| `sysctl`     | Enable IPv4 forwarding.                                         |
| `chown`      | Assign runtime file ownership.                                  |
| `sudo`       | Install missing utilities when the command is not already root. |

Supported automatic package managers are `apt-get`, `dnf`, `yum`, and `pacman`.
If your system uses another package manager, install the missing utilities
manually.

## Check Dependencies

Run:

```sh
bastion system check
```

The command renders a dependency tree and exits non-zero if any required item is
missing.

Use a custom data directory if needed:

```sh
bastion system --data-dir /var/lib/bastion check
```

## Install Cloud Hypervisor Assets

Run:

```sh
bastion system add cloud-hypervisor
```

If required utilities are missing, Bastion prompts before attempting to install
them. To skip the prompt:

```sh
bastion system add cloud-hypervisor --with-utilities
```

The command installs runtime assets under `<data-dir>/cloud-hypervisor`.

| Asset                   | Description                                           |
| ----------------------- | ----------------------------------------------------- |
| Cloud Hypervisor binary | Static VMM binary used by `bastiond`.                 |
| Guest kernel            | Ubuntu 24.04 kernel used to boot environments.        |
| Guest initramfs         | Matching initramfs for the guest kernel.              |
| Guest rootfs image      | Base Ubuntu image copied for each environment.        |
| SSH key                 | Host-side key used for root SSH into guests.          |
| Manifest                | Metadata for versions, paths, sources, and checksums. |

## Remove Cloud Hypervisor Assets

Run:

```sh
bastion system remove cloud-hypervisor
```

This removes Bastion-managed Cloud Hypervisor assets from the data directory. It
does not uninstall system utilities that were installed through the package
manager.

## Start the Runtime Services

`bastion start` runs the unprivileged host API:

```sh
bastion start
```

`bastiond` runs the privileged VM runtime:

```sh
sudo bastiond
```

Both processes must point at the same data directory and daemon socket. The
defaults are `~/.bastion` and `/run/bastion/bastiond.sock`.
