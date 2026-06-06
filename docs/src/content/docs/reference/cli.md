---
title: CLI Reference
description: Commands, flags, and environment variables for the Bastion CLI.
---

The `bastion` CLI either starts the host API or calls an already running host
API. JSON responses are written to stdout. Logs, streamed init output, and
diagnostics are written to stderr.

## Global Flags

| Flag        | Environment       | Default                 | Description                           |
| ----------- | ----------------- | ----------------------- | ------------------------------------- |
| `--api-url` | `BASTION_API_URL` | `http://localhost:3148` | Host API URL used by client commands. |

## `bastion start`

Starts the local host API service.

```sh
bastion start [flags]
```

| Flag                | Environment          | Default                      | Description                           |
| ------------------- | -------------------- | ---------------------------- | ------------------------------------- |
| `--addr`            | `BASTION_ADDR`       | `localhost:3148`             | Host API listen address.              |
| `--data-dir`        | `BASTION_DATA_DIR`   | `~/.bastion`                 | Persistent data directory.            |
| `--bastiond-socket` | `BASTIOND_SOCKET`    | `/run/bastion/bastiond.sock` | Unix socket used to reach `bastiond`. |
| `--log-format`      | `BASTION_LOG_FORMAT` | `json`                       | `json` or `text`.                     |
| `--log-level`       | `BASTION_LOG_LEVEL`  | `info`                       | `debug`, `info`, `warn`, or `error`.  |

## `bastion system`

Manages host dependencies and Cloud Hypervisor assets.

```sh
bastion system [--data-dir DIR] check
bastion system [--data-dir DIR] add cloud-hypervisor [--with-utilities]
bastion system [--data-dir DIR] remove cloud-hypervisor
```

| Flag               | Default      | Description                                                           |
| ------------------ | ------------ | --------------------------------------------------------------------- |
| `--data-dir`       | `~/.bastion` | Directory for system assets. Can also be set with `BASTION_DATA_DIR`. |
| `--with-utilities` | `false`      | Install missing supported utilities without prompting.                |

## `bastion templates`

Creates and manages environment templates.

```sh
bastion templates create [--key KEY] (--config JSON | --file PATH)
bastion templates list [--limit N] [--cursor CURSOR]
bastion templates get (--id ID | --key KEY)
bastion templates remove (--id ID | --key KEY)
```

| Command  | Description                                                      |
| -------- | ---------------------------------------------------------------- |
| `create` | Validate, initialize, snapshot, and store an immutable template. |
| `list`   | Return paginated template metadata.                              |
| `get`    | Return one template with full config.                            |
| `remove` | Delete one template by ID or key.                                |

Template keys are optional. When set, they must be unique. Unkeyed templates are
referenced by ID.

## `bastion env`

Creates and manages environments.

```sh
bastion env create (--template-id ID | --template-key KEY) [--key KEY] [--tag TAG...]
bastion env list [--limit N] [--cursor CURSOR] [--tag TAG...]
bastion env get (--id ID | --key KEY)
bastion env remove (--id ID | --key KEY)
```

| Command  | Description                                                            |
| -------- | ---------------------------------------------------------------------- |
| `create` | Restore a prepared template snapshot with an optional environment key. |
| `list`   | Return paginated environments, optionally filtered by repeated tags.   |
| `get`    | Return one environment after reconciling with the daemon.              |
| `remove` | Tear down and delete an environment.                                   |

Environment keys are optional. When set, they must be unique. `--template-key KEY`
requires a keyed template; use `--template-id ID` for unkeyed templates.

## `bastion ssh`

Connects to an environment through the host API.

```sh
bastion ssh (--id ID | --key KEY)
bastion ssh (--id ID | --key KEY) -- COMMAND [ARG...]
```

With no command and terminal stdin/stdout, the CLI opens an interactive PTY. With
a command, it forwards stdout, stderr, and the remote exit code.

## `bastion mux`

Opens a Bastion-managed tmux session for persistent SSH tabs.

```sh
bastion mux
```

The command creates or attaches to a `bastion` tmux session with Bastion's tmux
configuration loaded. New tabs open an environment picker menu populated from
`bastion env list`; use arrow keys and Enter to select an environment. Selecting
an environment replaces the tab with `bastion ssh --id ID`.

Tabs are named from the environment key, or the environment ID when no key is
set. Duplicate tabs connected to the same environment receive suffixes such as
`dev (2)` and `dev (3)`.

## `bastion version`

Prints the CLI version.

```sh
bastion version
```

Local development builds report `dev`. Release builds can inject a version at
build time.

## `bastiond`

`bastiond` is the privileged Cloud Hypervisor daemon. It is a separate binary and
must run as root.

```sh
sudo bastiond [flags]
```

| Flag           | Environment               | Default                            | Description                          |
| -------------- | ------------------------- | ---------------------------------- | ------------------------------------ |
| `--data-dir`   | `BASTION_DATA_DIR`        | `~/.bastion` for the sudo user     | Persistent data directory.           |
| `--socket`     | `BASTIOND_SOCKET`         | `/run/bastion/bastiond.sock`       | Unix socket path.                    |
| `--socket-uid` | `SUDO_UID` or current UID | User owner for the daemon socket.  |
| `--socket-gid` | `SUDO_GID` or current GID | Group owner for the daemon socket. |
| `--vm-uid`     | `BASTIOND_VM_UID`         | `0`                                | UID used for VM-owned runtime files. |
| `--vm-gid`     | `BASTIOND_VM_GID`         | `0`                                | GID used for VM-owned runtime files. |
| `--log-format` | `BASTIOND_LOG_FORMAT`     | `json`                             | `json` or `text`.                    |
| `--log-level`  | `BASTIOND_LOG_LEVEL`      | `info`                             | `debug`, `info`, `warn`, or `error`. |
