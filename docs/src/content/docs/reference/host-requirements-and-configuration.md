---
title: Host requirements and configuration
description: Supported platforms, runtime requirements, configuration precedence, and persistent paths for Bastion hosts and clusters.
---

Bastion separates the client, cluster control plane, host API, and virtual
machine runtime. Platform support and configuration depend on which role a
machine performs.

## Supported platforms

| Platform                    | Client commands | Cluster control plane | Host API and VM runtime |
| --------------------------- | --------------- | --------------------- | ----------------------- |
| Linux x86_64                | Supported       | Supported             | Supported               |
| macOS arm64 (Apple silicon) | Supported       | Supported             | Not supported           |

macOS arm64 can run `bastion start cluster` and can connect to a remote API. It
cannot run `bastion start api`, `bastion start daemon`, `bastion system`, or
host Cloud Hypervisor VMs. Those commands print a compatibility message instead.

The release installer supports Linux x86_64 host installs and macOS arm64 CLI
installs. A Bastion cluster can run its control plane on either supported
platform, but every node that hosts environments must be Linux x86_64.

> **Important:** The host and cluster APIs do not provide native authentication
> or TLS. Keep their default loopback listeners for local use. Put any API or
> node URL exposed beyond a trusted boundary behind authentication, TLS, request
> limits, and network access controls. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## Linux VM host requirements

A machine that runs `bastion start api` and `bastion start daemon` requires:

| Requirement             | Details                                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------------------------- |
| Operating system        | Linux                                                                                                 |
| Architecture            | x86_64                                                                                                |
| Hardware virtualization | `/dev/kvm` exists and opens for read and write                                                        |
| VM HTTP transport       | `/dev/vhost-vsock` exists                                                                             |
| Privilege               | `bastion start daemon` runs as root                                                                   |
| Networking              | The host can create TAP devices, enable IPv4 forwarding, and configure iptables NAT rules             |
| Storage                 | The data directory file system has capacity for the base, template overlays, and environment overlays |

Cloud instances must expose KVM. A Bastion host running inside another virtual
machine requires nested virtualization.

### What the system check verifies

`bastion system check` prints a dependency tree and verifies:

- The host reports Linux and x86_64.
- `/dev/kvm` exists and can be opened for read and write.
- `/dev/vhost-vsock` exists.
- Each utility in the following table resolves on `PATH`.
- Cloud Hypervisor, kernel, initramfs, root file system image, and SSH key files
  exist in the configured data directory. The Cloud Hypervisor file must be
  executable.
- The OpenCode manifest selects Bastion's pinned version and x86_64
  architecture, and its binary is executable. If the manifest names a retained
  archive, that archive must also exist.

| Utility      | Use                                                               |
| ------------ | ----------------------------------------------------------------- |
| `ssh-keygen` | Generate the guest SSH key.                                       |
| `ssh`        | Run guest lifecycle commands.                                     |
| `scp`        | Copy action packages and assets into guests.                      |
| `qemu-img`   | Create and inspect qcow2 disk layers.                             |
| `mkfs.vfat`  | Create cloud-init seed media.                                     |
| `mcopy`      | Write files to cloud-init seed media.                             |
| `dnsmasq`    | Provide DHCP for each VM TAP network.                             |
| `ip`         | Create and configure TAP interfaces and routes.                   |
| `iptables`   | Configure forwarding and NAT.                                     |
| `sysctl`     | Enable IPv4 forwarding.                                           |
| `chown`      | Set runtime file ownership.                                       |
| `sudo`       | Install missing packages when setup does not already run as root. |

`bastion system init` can install missing utilities with `apt-get`, `dnf`,
`yum`, or `pacman`. Other package managers require manual installation.

The check does not start Cloud Hypervisor or a guest. It does not verify asset
digests, utility versions or behavior, free storage, data-directory permissions,
daemon root privileges, `/dev/vhost-vsock` access, nested virtualization, TAP
creation, a default IPv4 route, forwarding or iptables behavior, subnet
conflicts, DHCP, guest SSH, DNS, or outbound connectivity. It also does not test
Postgres, S3-compatible storage, or either HTTP API.

Guest networking requires the daemon to create and configure a TAP device, find
a default IPv4 interface, enable IPv4 forwarding, and add forwarding and NAT
rules. Its per-VM `dnsmasq` advertises the host as the router and `8.8.8.8` and
`1.1.1.1` as DNS servers. The host firewall and upstream network must permit the
required forwarding, DNS, and outbound traffic. Actions that download packages
also require guest internet access. Verify these prerequisites separately by
creating a template or environment that exercises the required network path.

## Runtime processes

| Process                 | Default listener             | Persistent store                   | Role                                                                   |
| ----------------------- | ---------------------------- | ---------------------------------- | ---------------------------------------------------------------------- |
| `bastion start api`     | `localhost:3148`             | SQLite in the data directory       | Exposes one Linux host and calls the daemon.                           |
| `bastion start daemon`  | `/run/bastion/bastiond.sock` | VM artifacts in the data directory | Performs privileged Cloud Hypervisor, disk, TAP, and guest operations. |
| `bastion start cluster` | `localhost:3150`             | Postgres and S3-compatible storage | Coordinates namespaces and Linux host API nodes.                       |

The host API creates the top-level data directory. The daemon waits up to 30
seconds for that directory rather than creating it first. The host API and
daemon must use the same data directory and daemon socket.

The host API applies embedded SQLite migrations before serving. The cluster
control plane applies embedded Postgres migrations before serving. Startup
fails when migration or database setup fails.

## Configuration precedence

For a command option with both a flag and environment variable, the explicit
flag wins. The command-specific tables below show environment fallbacks in
their exact order.

Client options add a persisted configuration tier. API URL and namespace
selection use this order:

1. Explicit root flag.
2. Environment variable.
3. Persisted value in `<DATA_DIR>/client.json`.
4. Built-in default, when the option has one.

`DATA_DIR` means the resolved Bastion data directory. `~` and `~/...` are
expanded, and relative paths become absolute paths.

## Host API configuration

These settings apply to `bastion start api`. `--data-dir` is a root flag and can
appear before or after `start api`.

| Flag                            | Environment          | Default                      | Description                          |
| ------------------------------- | -------------------- | ---------------------------- | ------------------------------------ |
| `--addr ADDRESS`                | `BASTION_ADDR`       | `localhost:3148`             | Plain HTTP listen address.           |
| `--data-dir DATA_DIR`           | `BASTION_DATA_DIR`   | `~/.bastion`                 | Persistent host data directory.      |
| `--bastiond-socket SOCKET_PATH` | `BASTIOND_SOCKET`    | `/run/bastion/bastiond.sock` | Daemon Unix socket.                  |
| `--log-format FORMAT`           | `BASTION_LOG_FORMAT` | `json`                       | `json` or `text`.                    |
| `--log-level LEVEL`             | `BASTION_LOG_LEVEL`  | `info`                       | `debug`, `info`, `warn`, or `error`. |

`ADDRESS` is a Go listen address such as `localhost:3148` or `0.0.0.0:3148`.
The server itself speaks HTTP, not HTTPS.

Request logs go to stderr and include `request_id`, `method`, `route`, `status`,
`duration`, `client_ip`, and `body_size`.

## Daemon configuration

These settings apply to `sudo bastion start daemon`.

| Flag                   | Environment or source        | Default                            | Description                                         |
| ---------------------- | ---------------------------- | ---------------------------------- | --------------------------------------------------- |
| `--data-dir DATA_DIR`  | `BASTION_DATA_DIR`           | Invoking sudo user's `~/.bastion`  | Persistent VM and action data.                      |
| `--socket SOCKET_PATH` | `BASTIOND_SOCKET`            | `/run/bastion/bastiond.sock`       | Daemon Unix socket.                                 |
| `--socket-uid UID`     | `SUDO_UID`, then current UID | Current invoking user with `sudo`  | Owner of the daemon and per-VM proxy sockets.       |
| `--socket-gid GID`     | `SUDO_GID`, then current GID | Current invoking group with `sudo` | Group owner of the daemon and per-VM proxy sockets. |
| `--vm-uid UID`         | `BASTIOND_VM_UID`            | `0`                                | Owner UID for VM runtime files.                     |
| `--vm-gid GID`         | `BASTIOND_VM_GID`            | `0`                                | Owner GID for VM runtime files.                     |
| `--log-format FORMAT`  | `BASTIOND_LOG_FORMAT`        | `json`                             | `json` or `text`.                                   |
| `--log-level LEVEL`    | `BASTIOND_LOG_LEVEL`         | `info`                             | `debug`, `info`, `warn`, or `error`.                |

`UID` and `GID` are decimal operating-system user and group IDs. Invalid
environment values for numeric daemon options fall back to their defaults.

The socket owner also owns per-VM `vsock.socket` files and the base SSH private
key. Set the socket UID and GID so the host API service user can access them.

When neither `--data-dir` nor `BASTION_DATA_DIR` is set, a daemon started with
`sudo` resolves the home directory of `SUDO_USER`. Otherwise it uses the current
user's default data directory.

At startup, the daemon replaces directories in `<DATA_DIR>/actions` that have
built-in action names. Use unique names for custom actions. See the
[action manifest reference](/reference/action-manifest/).

## VM sizing configuration

Every process that interprets a template resolves omitted resources in its own
environment. Bastion does not resolve defaults once and persist the resulting
allocation:

| Process               | Use of resolved resources                                         |
| --------------------- | ----------------------------------------------------------------- |
| Host API              | Accounts for host utilization.                                    |
| Daemon                | Sizes the actual template-preparation VM or environment VM.       |
| Cluster control plane | Schedules source environments and accounts for source allocation. |

The host API, daemon, and cluster process can therefore resolve the same
template differently when their environments or versions differ. Template
values take precedence in each process.

| Resource    | Template field             | Process environment fallback        | Built-in default |
| ----------- | -------------------------- | ----------------------------------- | ---------------- |
| vCPU        | `resources.vcpu`           | `BASTION_VM_CPUS`                   | `2`              |
| Memory      | `resources.memory`, in GiB | `BASTION_VM_MEMORY_BYTES`, in bytes | `2 GiB`          |
| Root volume | `resources.volume`, in GiB | None                                | `20 GiB`         |

Positive template values override the corresponding environment values in all
three processes.
Missing, nonnumeric, or nonpositive `BASTION_VM_CPUS` and
`BASTION_VM_MEMORY_BYTES` values fall back to the built-in defaults. Template
volume has no environment fallback.

For predictable cluster scheduling and node behavior, explicitly set `vcpu`,
`memory`, and `volume` in every cluster template. Do not rely on process-local
defaults as a portable allocation contract. See the
[template configuration reference](/reference/template-configuration/).

Guests use `root` for SSH on port `22`. The managed OpenCode server uses port
`4096` unless `agents.opencode.config.server.port` specifies another port.

## VM network configuration

| Environment                 | Default  | Description                                                                       |
| --------------------------- | -------- | --------------------------------------------------------------------------------- |
| `BASTION_VM_NETWORK_PREFIX` | `10.241` | First two octets of the `/16` from which Bastion allocates per-VM `/30` networks. |

The value must contain exactly two valid IPv4 octets, for example `10.242`.
Bastion rejects a selected VM subnet when it overlaps an existing host route.
Choose a prefix that does not overlap host, VPN, container, or parent Bastion
networks. The daemon reads this setting when it performs VM operations.

## Cluster control plane configuration

These settings apply to `bastion start cluster`. The control plane runs on both
Linux and macOS arm64 and does not use the host data directory.

| Flag                                       | Environment fallback order                                      | Default                                                                     | Description                                          |
| ------------------------------------------ | --------------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------- |
| `--addr ADDRESS`                           | `BASTION_CLUSTER_ADDR`                                          | `localhost:3150`                                                            | Plain HTTP cluster API listen address.               |
| `--database-url DATABASE_URL`              | `BASTION_CLUSTER_DATABASE_URL`, `DATABASE_URL`                  | `postgres://bastion:bastion@localhost:3151/bastion_cluster?sslmode=disable` | Postgres connection URL.                             |
| `--s3-bucket BUCKET`                       | `BASTION_CLUSTER_S3_BUCKET`                                     | None                                                                        | S3-compatible bucket for base and template archives. |
| `--s3-endpoint ENDPOINT_URL`               | `BASTION_CLUSTER_S3_ENDPOINT`                                   | Provider default                                                            | Optional S3-compatible endpoint.                     |
| `--s3-region REGION`                       | `BASTION_CLUSTER_S3_REGION`                                     | `us-east-1`                                                                 | S3 region.                                           |
| `--s3-access-key-id ACCESS_KEY_ID`         | `BASTION_CLUSTER_S3_ACCESS_KEY_ID`, `AWS_ACCESS_KEY_ID`         | AWS SDK credential chain                                                    | S3 access key ID.                                    |
| `--s3-secret-access-key SECRET_ACCESS_KEY` | `BASTION_CLUSTER_S3_SECRET_ACCESS_KEY`, `AWS_SECRET_ACCESS_KEY` | AWS SDK credential chain                                                    | S3 secret access key.                                |
| `--s3-use-path-style`                      | `BASTION_CLUSTER_S3_USE_PATH_STYLE`                             | `false`                                                                     | Use path-style S3 URLs.                              |
| `--log-format FORMAT`                      | `BASTION_CLUSTER_LOG_FORMAT`, `BASTION_LOG_FORMAT`              | `json`                                                                      | `json` or `text`.                                    |
| `--log-level LEVEL`                        | `BASTION_CLUSTER_LOG_LEVEL`, `BASTION_LOG_LEVEL`                | `info`                                                                      | `debug`, `info`, `warn`, or `error`.                 |

`DATABASE_URL`, `ENDPOINT_URL`, and the other uppercase values are placeholders,
not literal values. Boolean environment values accept Go boolean syntax; an
invalid `BASTION_CLUSTER_S3_USE_PATH_STYLE` value falls back to `false`.

> **Warning:** Cluster startup can expose database credentials in logs.
>
> At `info` and `debug` levels, it logs the full resolved database URL in the
> `database_url` field. Until the core service redacts it, restrict access to log
> destinations and use `--log-level warn` or `--log-level error` when startup
> details are not required.

Archive storage is configured only when `BUCKET` is nonempty. Node, namespace,
health API, utilization API, and secret operations remain available without it.
Base build, import, and export; template create, import, export, and delete; and
environment creation return `424 Failed Dependency` when they require an
archive store and none is configured.

If either resolved S3 access-key value is nonempty, Bastion configures a static
credential provider from the two access-key values. Values inherited from
`AWS_ACCESS_KEY_ID` or `AWS_SECRET_ACCESS_KEY` count as explicitly set for this
decision. This static provider always uses an empty session token, so Bastion
ignores `AWS_SESSION_TOKEN` in this mode.

If temporary credentials depend on a session token, leave both Bastion S3
access-key variables and the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`
fallbacks unset. Use another AWS SDK chain source, such as a shared profile, web
identity, container credentials, or an instance role.

## CLI client configuration

These root settings apply to commands that call a host or cluster API:

| Flag                            | Environment             | Persisted field | Default                                                                                   |
| ------------------------------- | ----------------------- | --------------- | ----------------------------------------------------------------------------------------- |
| `--api-url API_URL`             | `BASTION_API_URL`       | `apiUrl`        | `http://localhost:3148`, except `bastion cluster ...` defaults to `http://localhost:3150` |
| `--namespace-id NAMESPACE_ID`   | `BASTION_NAMESPACE_ID`  | `namespaceId`   | None                                                                                      |
| `--namespace-key NAMESPACE_KEY` | `BASTION_NAMESPACE_KEY` | `namespaceKey`  | None                                                                                      |
| `--data-dir DATA_DIR`           | `BASTION_DATA_DIR`      | Not persisted   | `~/.bastion`                                                                              |

Use an absolute `http` or `https` `API_URL` with a host and an optional base
path. Do not include user information, a query, or a fragment because Bastion
appends product routes by string concatenation. Current validation does not
reject every unsupported component. Follow the same rule for registered cluster
node URLs.

Only the `bastion cluster nodes ...` and `bastion cluster namespaces ...`
command trees receive the cluster URL default. Shared commands such as
`bastion base`, `bastion secrets`, and `bastion env` keep the host URL default,
even when they can operate against a cluster. Set `--api-url`,
`BASTION_API_URL`, or a persisted API URL for those commands.

Namespace selection applies only to secrets, templates, environments, SSH,
tunnels, proxy, OpenCode, and mux resource requests. The base, health API,
utilization API, node management, and namespace management remain global.
Exactly one namespace ID or key can be active.

At each precedence tier, a namespace ID and key conflict with one another. A
flag-tier selection overrides all environment and persisted namespace values;
an environment-tier selection overrides persisted values.

`bastion client set namespace-id` clears the persisted namespace key, and
`bastion client set namespace-key` clears the persisted namespace ID. A newly
created `client.json` requests mode `0600`. Updating it preserves existing
permissions. It does not replace the mode of an existing file. Restrict an
existing file separately if it has broader permissions. See the
[host CLI reference](/reference/cli/host/#bastion-client-commands) and
[cluster CLI reference](/reference/cli/cluster/#namespace-selection-for-resource-commands).

## Systemd service configuration

The Linux installer creates `bastion-api.service` and `bastiond.service`. Both
read `/etc/default/bastion`. The installer creates that file once and preserves
it on later installs.

The initial file sets:

```text
BASTION_ADDR="localhost:3148"
BASTION_DATA_DIR="/home/example/.bastion"
BASTIOND_SOCKET="/run/bastion/bastiond.sock"
BASTION_LOG_FORMAT="json"
BASTION_LOG_LEVEL="info"
BASTIOND_LOG_FORMAT="json"
BASTIOND_LOG_LEVEL="info"
```

`/home/example/.bastion` is an example resolved data directory. Restart both
services after changing shared runtime settings. `bastiond.service` uses
`KillMode=process`, so restarting the daemon does not automatically terminate
Cloud Hypervisor child processes.

## Persistent data layout

The default data directory contains:

```text
~/.bastion/
|-- client.json
|-- sqlite.db
|-- actions/
|-- cloud-hypervisor/
|-- opencode/
|-- base/
|-- templates/
`-- environments/
```

| Path                                       | Contents                                                               |
| ------------------------------------------ | ---------------------------------------------------------------------- |
| `<DATA_DIR>/client.json`                   | Local CLI API URL and namespace overrides.                             |
| `<DATA_DIR>/sqlite.db`                     | Host API resource metadata.                                            |
| `<DATA_DIR>/actions`                       | Seeded built-in and user-created action packages.                      |
| `<DATA_DIR>/cloud-hypervisor`              | VMM binary, kernel, initramfs, source image, SSH key, and manifest.    |
| `<DATA_DIR>/opencode`                      | Pinned OpenCode guest asset and manifest.                              |
| `<DATA_DIR>/base`                          | Prepared shared base disk, cloud-init seed, SSH key, and metadata.     |
| `<DATA_DIR>/templates/<TEMPLATE_ID>`       | Immutable template overlay and metadata.                               |
| `<DATA_DIR>/environments/<ENVIRONMENT_ID>` | Writable environment overlay, cloud-init media, logs, and VM metadata. |
| `/run/bastion/bastiond.sock`               | Default daemon Unix socket.                                            |
| `/run/bastion/vms/<VM_ID>`                 | Live VM runtime symlink and socket paths.                              |

`TEMPLATE_ID`, `ENVIRONMENT_ID`, and `VM_ID` represent generated resource or VM
identifiers. The cluster control plane stores source metadata in Postgres and
base/template archives in its configured S3-compatible bucket; it does not use
this host data layout for cluster source resources.
