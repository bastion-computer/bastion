---
title: System Setup
description: Host requirements and Cloud Hypervisor asset management.
---

Bastion launches environments with Cloud Hypervisor. The `bastion system`
commands check and install the host-side dependencies and source assets needed
by that runtime. They do not build the shared [base image](/guides/base/).

## Host Requirements

The current runtime expects:

| Requirement       | Details                                    |
| ----------------- | ------------------------------------------ |
| Operating system  | Linux                                      |
| Architecture      | x86_64                                     |
| Virtualization    | `/dev/kvm` exists and is readable/writable |
| Privileged daemon | `bastion start daemon` runs as root        |

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
| `dnsmasq`    | Provide DHCP for each VM's TAP network.                         |
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

The command renders a dependency tree, including pinned asset versions, and exits
non-zero if any required item is missing.

Use a custom data directory if needed:

```sh
bastion system --data-dir /var/lib/bastion check
```

## Initialize System Dependencies

Run:

```sh
bastion system init
```

If required utilities are missing, Bastion prompts before attempting to install
them. To skip the prompt:

```sh
bastion system init --with-utilities
```

The command installs Cloud Hypervisor assets under
`<data-dir>/cloud-hypervisor` and pinned OpenCode assets under
`<data-dir>/opencode`.

| Asset                   | Description                                                           |
| ----------------------- | --------------------------------------------------------------------- |
| Cloud Hypervisor binary | Static VMM binary used by the daemon.                                 |
| Guest kernel            | Ubuntu 24.04 kernel used to boot base, template, and environment VMs. |
| Guest initramfs         | Matching initramfs for the guest kernel.                              |
| Guest rootfs image      | Source Ubuntu image copied and prepared by `bastion base build`.      |
| SSH key                 | Source key copied into a locally built base for guest root SSH.       |
| OpenCode asset          | Pinned binary installed into the guest during base build.             |
| Manifests               | Metadata for versions, paths, sources, and checksums.                 |

## Clean System Dependencies

Run:

```sh
bastion system clean
```

This removes Bastion-managed Cloud Hypervisor and OpenCode assets from the data
directory. It does not remove bases, templates, environments, host metadata, or
system utilities installed through the package manager.

## Start the Runtime Services

:::note
This can be skipped if installing via the [installer script](/quick-start/#install-bastion).
:::

`bastion start api` runs the unprivileged host API:

```sh
bastion start api
```

`bastion start daemon` runs the privileged VM runtime:

```sh
sudo bastion start daemon
```

Both processes must point at the same data directory and daemon socket. The
defaults are `~/.bastion` and `/run/bastion/bastiond.sock`.

## Build the base image

Once both services are running, [build or import the base](/guides/base/) before
creating templates:

```sh
bastion base build
```
