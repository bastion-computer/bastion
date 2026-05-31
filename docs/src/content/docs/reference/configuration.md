---
title: Configuration
description: Runtime paths, environment variables, and VM defaults.
---

Bastion configuration is provided with CLI flags and environment variables. CLI
flags take precedence over environment defaults.

## Host API

These settings apply to `bastion start`.

| Flag                 | Environment          | Default                      | Description                          |
| -------------------- | -------------------- | ---------------------------- | ------------------------------------ |
| `--addr`             | `BASTION_ADDR`       | `localhost:3148`             | Host API listen address.             |
| `--data-dir`         | `BASTION_DATA_DIR`   | `~/.bastion`                 | Persistent data directory.           |
| `--bastiond-socket`  | `BASTIOND_SOCKET`    | `/run/bastion/bastiond.sock` | Unix socket used to call `bastiond`. |
| `--queue-proxy-port` | `QUEUE_PROXY_PORT`   | `3150`                       | Queue proxy port on VM TAP host IPs. |
| `--log-format`       | `BASTION_LOG_FORMAT` | `json`                       | `json` or `text`.                    |
| `--log-level`        | `BASTION_LOG_LEVEL`  | `info`                       | `debug`, `info`, `warn`, or `error`. |

The host API creates the data directory if needed and stores SQLite data at
`<data-dir>/sqlite.db`.

## Systemd Services

When installed with `--with-services`, Bastion creates `bastion-api.service` and
`bastiond.service`. Both units read service environment values from
`/etc/default/bastion`.

The installer seeds `/etc/default/bastion` on first service setup and preserves
the file during later installs or updates. Edit this file to customize values
such as `BASTION_DATA_DIR`, `BASTION_ADDR`, and `BASTIOND_SOCKET`, then restart
the services:

```sh
sudo systemctl restart bastiond.service bastion-api.service
```

## CLI Client

These settings apply to CLI commands that call the host API.

| Flag        | Environment       | Default                 | Description        |
| ----------- | ----------------- | ----------------------- | ------------------ |
| `--api-url` | `BASTION_API_URL` | `http://localhost:3148` | Host API base URL. |

## Daemon

These settings apply to `bastiond`.

| Flag           | Environment               | Default                        | Description                          |
| -------------- | ------------------------- | ------------------------------ | ------------------------------------ |
| `--data-dir`   | `BASTION_DATA_DIR`        | `~/.bastion` for the sudo user | Persistent data directory.           |
| `--socket`     | `BASTIOND_SOCKET`         | `/run/bastion/bastiond.sock`   | Unix socket path.                    |
| `--socket-uid` | `SUDO_UID` or current UID | Socket owner UID.              |
| `--socket-gid` | `SUDO_GID` or current GID | Socket owner GID.              |
| `--vm-uid`     | `BASTIOND_VM_UID`         | `0`                            | UID used for VM-owned runtime files. |
| `--vm-gid`     | `BASTIOND_VM_GID`         | `0`                            | GID used for VM-owned runtime files. |
| `--log-format` | `BASTIOND_LOG_FORMAT`     | `json`                         | `json` or `text`.                    |
| `--log-level`  | `BASTIOND_LOG_LEVEL`      | `info`                         | `debug`, `info`, `warn`, or `error`. |

`bastiond` must run as root. When run with `sudo`, it defaults its data
directory to the invoking user's home directory rather than root's home.

## VM Defaults

These values are used by the Cloud Hypervisor runtime.

| Setting  | Default  | Description                                                             |
| -------- | -------- | ----------------------------------------------------------------------- |
| vCPU     | `2`      | Overridden by template `resources.vcpu` or `BASTION_VM_CPUS`.           |
| Memory   | `2 GiB`  | Overridden by template `resources.memory` or `BASTION_VM_MEMORY_BYTES`. |
| Volume   | `20 GiB` | Overridden by template `resources.volume`.                              |
| SSH user | `root`   | Provisioned guest user.                                                 |
| SSH port | `22`     | Guest SSH port.                                                         |

Template resource values are usually the right way to size environments because
they travel with the template definition.

## Queue Proxy

Environment functions poll queues through a lightweight proxy bound to each VM's
TAP host IP. Configure the port with `QUEUE_PROXY_PORT`; the default is `3150`.
The main host API can remain bound to `localhost`.

## VM Networking

Bastion creates a TAP interface per VM and configures forwarding and NAT through
iptables.

| Environment                 | Default  | Description                                                |
| --------------------------- | -------- | ---------------------------------------------------------- |
| `BASTION_VM_NETWORK_PREFIX` | `10.241` | IPv4 `/16` prefix used to carve out per-VM `/30` networks. |

The prefix must look like two IPv4 octets, for example `10.242`.

## Data Layout

Default data directory:

```text
~/.bastion/
├── sqlite.db
├── actions/
├── functions/
├── cloud-hypervisor/
└── environments/
```

Runtime files live under `/run/bastion` by default:

```text
/run/bastion/
├── bastiond.sock
└── vms/
```

Important paths:

| Path                               | Description                                                   |
| ---------------------------------- | ------------------------------------------------------------- |
| `<data-dir>/sqlite.db`             | Host API metadata database.                                   |
| `<data-dir>/actions`               | Built-in and custom action packages.                          |
| `<data-dir>/functions`             | Custom TypeScript function packages.                          |
| `<data-dir>/cloud-hypervisor`      | Cloud Hypervisor binary, guest images, SSH key, and manifest. |
| `<data-dir>/environments/<env-id>` | Persistent per-environment VM files and metadata.             |
| `/run/bastion/vms/<vm-id>`         | Runtime symlink and socket files for live VMs.                |
| `/run/bastion/bastiond.sock`       | Default daemon Unix socket.                                   |
