---
title: CLI Reference
description: Commands, flags, and environment variables for the Bastion CLI.
---

The `bastion` CLI starts Bastion services or calls an already running host or
cluster API. JSON responses are written to stdout. Logs, streamed init output,
and diagnostics are written to stderr.

## Global Flags

| Flag              | Environment             | Default                 | Description                                          |
| ----------------- | ----------------------- | ----------------------- | ---------------------------------------------------- |
| `--api-url`       | `BASTION_API_URL`       | `http://localhost:3148` | API URL used by client commands.                     |
| `--data-dir`      | `BASTION_DATA_DIR`      | `~/.bastion`            | Persistent data directory and client config storage. |
| `--namespace-id`  | `BASTION_NAMESPACE_ID`  |                         | Cluster namespace ID for resource commands.          |
| `--namespace-key` | `BASTION_NAMESPACE_KEY` |                         | Cluster namespace key for resource commands.         |

Client commands resolve `--api-url` in this order: explicit flag,
`BASTION_API_URL`, `<data-dir>/client.json`, then the built-in default.
`bastion cluster` defaults to the local cluster API at `http://localhost:3150`;
other client commands default to the local host API at `http://localhost:3148`.

Cluster namespace selectors resolve in this order: explicit `--namespace-id` or
`--namespace-key`, `BASTION_NAMESPACE_ID` or `BASTION_NAMESPACE_KEY`, then
`<data-dir>/client.json`. Specify only one namespace selector at a time.

## `bastion start`

Starts a Bastion process. Specify a process type: `api`, `cluster`, or `daemon`.

On macOS, `bastion start api` and `bastion start daemon` print a compatibility
message. Use `--api-url` to connect the macOS CLI to a remote Linux Bastion host
API.

### `bastion start api`

Starts the local host API service.

```sh
bastion start api [flags]
```

| Flag                | Environment          | Default                      | Description                           |
| ------------------- | -------------------- | ---------------------------- | ------------------------------------- |
| `--addr`            | `BASTION_ADDR`       | `localhost:3148`             | Host API listen address.              |
| `--data-dir`        | `BASTION_DATA_DIR`   | `~/.bastion`                 | Persistent data directory.            |
| `--bastiond-socket` | `BASTIOND_SOCKET`    | `/run/bastion/bastiond.sock` | Unix socket used to reach the daemon. |
| `--log-format`      | `BASTION_LOG_FORMAT` | `json`                       | `json` or `text`.                     |
| `--log-level`       | `BASTION_LOG_LEVEL`  | `info`                       | `debug`, `info`, `warn`, or `error`.  |

### `bastion start cluster`

Starts the cluster control plane API service.

```sh
bastion start cluster [flags]
```

| Flag                     | Environment                                                          | Default                                                                     | Description                              |
| ------------------------ | -------------------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------- |
| `--addr`                 | `BASTION_CLUSTER_ADDR`                                               | `localhost:3150`                                                            | Cluster API listen address.              |
| `--database-url`         | `BASTION_CLUSTER_DATABASE_URL`, then `DATABASE_URL`                  | `postgres://bastion:bastion@localhost:3151/bastion_cluster?sslmode=disable` | Cluster Postgres database URL.           |
| `--s3-bucket`            | `BASTION_CLUSTER_S3_BUCKET`                                          |                                                                             | S3 bucket for cluster template archives. |
| `--s3-endpoint`          | `BASTION_CLUSTER_S3_ENDPOINT`                                        |                                                                             | S3-compatible endpoint URL.              |
| `--s3-region`            | `BASTION_CLUSTER_S3_REGION`                                          | `us-east-1`                                                                 | S3 region for template archives.         |
| `--s3-access-key-id`     | `BASTION_CLUSTER_S3_ACCESS_KEY_ID`, then `AWS_ACCESS_KEY_ID`         |                                                                             | S3 access key ID.                        |
| `--s3-secret-access-key` | `BASTION_CLUSTER_S3_SECRET_ACCESS_KEY`, then `AWS_SECRET_ACCESS_KEY` |                                                                             | S3 secret access key.                    |
| `--s3-use-path-style`    | `BASTION_CLUSTER_S3_USE_PATH_STYLE`                                  | `false`                                                                     | Use path-style S3 URLs.                  |
| `--log-format`           | `BASTION_CLUSTER_LOG_FORMAT`, then `BASTION_LOG_FORMAT`              | `json`                                                                      | `json` or `text`.                        |
| `--log-level`            | `BASTION_CLUSTER_LOG_LEVEL`, then `BASTION_LOG_LEVEL`                | `info`                                                                      | `debug`, `info`, `warn`, or `error`.     |

Configure S3 storage before using cluster template and environment
orchestration. Without template archive storage, node and namespace management
can still run, but template archive operations fail.

### `bastion start daemon`

Starts the privileged Cloud Hypervisor daemon. It must run as root.

```sh
sudo bastion start daemon [flags]
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

The socket owner/group also owns per-VM proxy sockets used by OpenCode and
environment tunnels, so it should match the user running `bastion start api`.

## `bastion system`

Manages host dependencies and Cloud Hypervisor assets.

On macOS, `bastion system` and its subcommands are no-ops that print a
compatibility message because Cloud Hypervisor host setup is Linux-only.

```sh
bastion system [--data-dir DIR] check
bastion system [--data-dir DIR] init [--with-utilities]
bastion system [--data-dir DIR] clean
```

| Flag               | Default      | Description                                                           |
| ------------------ | ------------ | --------------------------------------------------------------------- |
| `--data-dir`       | `~/.bastion` | Directory for system assets. Can also be set with `BASTION_DATA_DIR`. |
| `--with-utilities` | `false`      | Install missing supported utilities without prompting.                |

## `bastion client`

Manages persistent local CLI client configuration.

```sh
bastion client [--data-dir DIR] set api-url URL
bastion client [--data-dir DIR] set namespace-id ID
bastion client [--data-dir DIR] set namespace-key KEY
bastion client [--data-dir DIR] remove api-url
bastion client [--data-dir DIR] remove namespace-id
bastion client [--data-dir DIR] remove namespace-key
bastion client [--data-dir DIR] config
```

| Command                | Description                                                      |
| ---------------------- | ---------------------------------------------------------------- |
| `set api-url`          | Persist the API URL used when no `--api-url` flag or env is set. |
| `set namespace-id`     | Persist the cluster namespace ID used by resource commands.      |
| `set namespace-key`    | Persist the cluster namespace key used by resource commands.     |
| `remove api-url`       | Remove the persisted API URL override.                           |
| `remove namespace-id`  | Remove the persisted cluster namespace ID override.              |
| `remove namespace-key` | Remove the persisted cluster namespace key override.             |
| `config`               | Print resolved client flag values and their sources as JSON.     |

Overrides are stored in `<data-dir>/client.json`. Use this when connecting to a
remote Bastion API often enough that passing `--api-url` every time is noisy:

```sh
bastion client set api-url https://bastion.example
bastion env list
```

Persist a cluster API URL and namespace for resource commands:

```sh
bastion client set api-url https://cluster.example
bastion client set namespace-key team-a
bastion templates list
```

Setting `namespace-id` clears any persisted `namespace-key`, and setting
`namespace-key` clears any persisted `namespace-id`.

Use `--data-dir` to keep separate client profiles:

```sh
bastion client --data-dir ~/.bastion-remote set api-url https://bastion.example
bastion --data-dir ~/.bastion-remote env list
```

## `bastion cluster`

Manages cluster control plane resources. These commands call the cluster API and
default to `http://localhost:3150` when no `--api-url`, `BASTION_API_URL`, or
client config value is set.

Namespace flags are not applied to `bastion cluster` management routes.

### `bastion cluster nodes`

Registers Bastion host API nodes that can run environments.

```sh
bastion cluster nodes create [--key KEY] --url URL
bastion cluster nodes list [--limit N] [--cursor CURSOR]
bastion cluster nodes get (--id ID | --key KEY)
bastion cluster nodes remove (--id ID | --key KEY)
```

| Command  | Description                             |
| -------- | --------------------------------------- |
| `create` | Add a Bastion API node to the cluster.  |
| `list`   | Return paginated cluster node metadata. |
| `get`    | Return one cluster node by ID or key.   |
| `remove` | Remove one cluster node by ID or key.   |

Node URLs must be absolute `http` or `https` URLs reachable from the cluster
control plane. Node IDs start with `node_`. Node keys are optional, unique, and
cannot start with the reserved `node_` prefix.

### `bastion cluster namespaces`

Creates and manages resource isolation namespaces for cluster secrets,
templates, and environments.

```sh
bastion cluster namespaces create [--key KEY]
bastion cluster namespaces list [--limit N] [--cursor CURSOR]
bastion cluster namespaces get (--id ID | --key KEY)
bastion cluster namespaces remove (--id ID | --key KEY)
```

| Command  | Description                                  |
| -------- | -------------------------------------------- |
| `create` | Create a cluster namespace.                  |
| `list`   | Return paginated cluster namespace metadata. |
| `get`    | Return one cluster namespace by ID or key.   |
| `remove` | Remove one cluster namespace by ID or key.   |

Namespace IDs start with `ns_`. Namespace keys are optional, unique, and cannot
start with the reserved `ns_` prefix. When using the cluster API for `secrets`,
`templates`, `env`, `ssh`, `proxy`, or `opencode`, select a namespace with
`--namespace-id`, `--namespace-key`, environment variables, or persisted client
config. The CLI sends cluster resource requests under `/v1/namespaces/:id/...`
or `/v1/namespaces/by-key/:key/...`.

## `bastion utilization`

Shows host capacity and current allocations for live environments. When pointed
at the cluster API, shows aggregate cluster capacity.

```sh
bastion utilization
```

The command calls `GET /v1/utilization` and writes JSON to stdout. `memory` and
`volume` values are bytes. Used capacity includes environments in `creating`,
`running`, and `paused` states. Against the cluster API, the response aggregates
capacity across registered nodes.

## `bastion secrets`

Creates and manages secrets referenced by templates.

```sh
bastion secrets create [--key KEY] --value VALUE
bastion secrets list [--limit N] [--cursor CURSOR]
bastion secrets get (--id ID | --key KEY)
bastion secrets remove (--id ID | --key KEY)
```

| Command  | Description                                      |
| -------- | ------------------------------------------------ |
| `create` | Store a secret value and return metadata only.   |
| `list`   | Return paginated secret metadata without values. |
| `get`    | Return one secret with its value.                |
| `remove` | Delete one secret by ID or key.                  |

Secret IDs start with `sec_`. Secret keys are optional. When set, they must be
unique and cannot start with `sec_`. Templates can reference secrets with
`${{ secret.KEY }}` or `${{ secret.sec_xxxxxx }}`.

## `bastion templates`

Creates and manages environment templates.

```sh
bastion templates create [--key KEY] (--config JSON | --file PATH)
bastion templates list [--limit N] [--cursor CURSOR]
bastion templates get (--id ID | --key KEY)
bastion templates export (--id ID | --key KEY) > template.tar.zst
bastion templates import [--key KEY] --file PATH
bastion templates remove (--id ID | --key KEY)
```

| Command  | Description                                                      |
| -------- | ---------------------------------------------------------------- |
| `create` | Validate, initialize, snapshot, and store an immutable template. |
| `list`   | Return paginated template metadata.                              |
| `get`    | Return one template with full config.                            |
| `export` | Stream a prepared template archive to stdout by ID or key.       |
| `import` | Upload a prepared template archive and create a new template.    |
| `remove` | Delete one template by ID or key.                                |

Template keys are optional. When set, they must be unique. Unkeyed templates are
referenced by ID.

Imports never preserve the exported template ID or key. Use `--key` on import to
assign a new key to the restored template.

## `bastion env`

Creates and manages environments.

```sh
bastion env create (--template-id ID | --template-key KEY) [--key KEY] [--tag TAG...]
bastion env list [--limit N] [--cursor CURSOR] [--tag TAG...]
bastion env get (--id ID | --key KEY)
bastion env tunnels (--id ID | --key KEY)
bastion env remove (--id ID | --key KEY)
```

| Command   | Description                                                            |
| --------- | ---------------------------------------------------------------------- |
| `create`  | Restore a prepared template snapshot with an optional environment key. |
| `list`    | Return paginated environments, optionally filtered by repeated tags.   |
| `get`     | Return one environment after reconciling with the daemon.              |
| `tunnels` | Return registered tunnel ports and API URLs.                           |
| `remove`  | Tear down and delete an environment.                                   |

Environment keys are optional. When set, they must be unique. `--template-key KEY`
requires a keyed template; use `--template-id ID` for unkeyed templates.

`bastion env tunnels` uses the resolved API URL from `--api-url`,
`BASTION_API_URL`, or `bastion client set api-url` when printing tunnel URLs.

## `bastion proxy`

Starts a local proxy on `localhost` by default for a named environment tunnel.

```sh
bastion proxy (--env-id ID | --env-key KEY) --name NAME [--host HOST] [--port PORT]
```

| Flag        | Default     | Description                                            |
| ----------- | ----------- | ------------------------------------------------------ |
| `--env-id`  |             | Environment ID. Mutually exclusive with `--env-key`.   |
| `--env-key` |             | Environment key. Mutually exclusive with `--env-id`.   |
| `--name`    |             | Registered tunnel name, such as `frontend`.            |
| `--host`    | `localhost` | Local host to serve, such as `127.0.0.1` or `0.0.0.0`. |
| `--port`    | Tunnel port | Local port. `0` selects a free port.                   |

The command validates that the environment exposes the named tunnel, then prints
the local URL and request logs to stderr. All local paths and HTTP methods are
forwarded to the API tunnel URL using the resolved `--api-url`. If the tunnel
port is unavailable locally, the command falls back to a free port and logs the
fallback to stderr.

Use this for web apps that expect absolute routes from the origin:

```sh
bastion proxy --env-key review-123 --name frontend
```

Use `--host 0.0.0.0` to serve the proxy on all interfaces.

## `bastion ssh`

Connects to an environment through the configured API.

```sh
bastion ssh (--id ID | --key KEY)
bastion ssh (--id ID | --key KEY) -- COMMAND [ARG...]
```

With no command and terminal stdin/stdout, the CLI opens an interactive PTY. With
a command, it forwards stdout, stderr, and the remote exit code.

## `bastion opencode`

Starts the local OpenCode TUI and attaches it to an OpenCode server running in an
environment through the Bastion API proxy.

```sh
bastion opencode (--id ID | --key KEY)
```

The command requires `opencode` to be installed locally and runs
`opencode attach PROXY_URL` under the hood. `PROXY_URL` points at the proxied
OpenCode agent endpoint for the selected environment.

## `bastion mux`

Opens a Bastion-managed tmux session for persistent environment tabs.

```sh
bastion mux
```

The command creates or attaches to a `bastion` tmux session with Bastion's tmux
configuration loaded. New tabs open an environment picker menu populated from
`bastion env list`; use arrow keys and Enter to select an environment. Selecting
an environment opens a second menu for the connection mode. Selecting `SSH`
replaces the tab with `bastion ssh --id ID`; selecting `OpenCode` replaces it
with `bastion opencode --id ID`.

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
