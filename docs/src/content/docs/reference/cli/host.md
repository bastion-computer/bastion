---
title: Host CLI
description: Commands, flags, prerequisites, output, and limitations for a Bastion host and client.
---

The `bastion` binary starts local services and calls host or cluster HTTP APIs.
This page documents local host operations and commands against a single host
API. For starting the control plane, managing nodes and namespaces, and using
namespaced resources, see the [cluster CLI reference](/reference/cli/cluster/).

Linux x86_64 supports every command on this page. macOS arm64 supports client
commands but cannot run the host API, daemon, system setup, or VMs. It can also
run the cluster control plane.

## Command conventions

Synopses use the following common uppercase placeholders:

| Placeholder                   | Value                                                   |
| ----------------------------- | ------------------------------------------------------- |
| `API_URL`                     | Absolute `http` or `https` API base URL.                |
| `DATA_DIR`                    | Local Bastion data or client-profile directory.         |
| `RESOURCE_ID`                 | Generated resource ID, such as an `env_`-prefixed UUID. |
| `RESOURCE_KEY`                | User-provided nonblank resource key.                    |
| `CURSOR`                      | Opaque pagination value from a previous response.       |
| `ARCHIVE_PATH` or `FILE_PATH` | Local file system path.                                 |
| `COMMAND` and `ARG`           | Remote command and zero or more arguments.              |

Brackets indicate optional syntax. A vertical bar indicates mutually exclusive
choices.

All commands support `-h` and `--help`. Cobra also generates `bastion help
[COMMAND]` and `bastion completion [bash|fish|powershell|zsh]`. Completion
commands write a shell script to stdout and do not call a Bastion API. This page
does not repeat those standard generated commands under every command family.

Resource commands write indented JSON to stdout. Logs, progress events,
diagnostics, and local proxy messages go to stderr. Archive export writes binary
data to stdout; redirect it to a file or pipe. Local setup commands write text
to stdout and errors to stderr.

> **Warning:** Arguments can be visible in process listings and shell history.
> This includes `secrets create --value`, inline template JSON, database URLs,
> S3 access keys, and S3 secret keys. `secrets get` writes the secret value to
> stdout, where terminals, logs, pipes, and redirections can expose it. Bastion
> does not currently provide a stdin value option for secret creation. Restrict
> the client machine, avoid recording sensitive invocations, and write sensitive
> stdout only to a protected destination.

## Global client flags

```sh
bastion [--api-url API_URL] [--data-dir DATA_DIR] \
  [--namespace-id NAMESPACE_ID | --namespace-key NAMESPACE_KEY] COMMAND
```

`NAMESPACE_ID` and `NAMESPACE_KEY` identify a cluster namespace and do not apply
to a single host API. See the canonical
[CLI client configuration](/reference/host-requirements-and-configuration/#cli-client-configuration)
for every environment variable, persisted field, default, and precedence rule.

Use an absolute `http` or `https` `API_URL` with a host and an optional base
path. Do not include user information, a query, or a fragment because Bastion
appends product routes by string concatenation. Current validation does not
reject every unsupported component.

Use slash-free resource keys. The schema permits `/`, but a by-key request path
cannot reliably address a slash-containing key, even when escaped. Use the
generated slash-free ID for an existing affected resource when an ID selector is
available.

> **Important:** `https` in `API_URL` requires a TLS endpoint outside Bastion.
> The native host API provides neither authentication nor TLS. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## `bastion start api` command

Starts the Linux host HTTP API.

```sh
bastion [--data-dir DATA_DIR] start api \
  [--addr ADDRESS] [--bastiond-socket SOCKET_PATH] \
  [--log-format FORMAT] [--log-level LEVEL]
```

`FORMAT` is `json` or `text`. `LEVEL` is `debug`, `info`, `warn`, or `error`.
The command creates `DATA_DIR`, opens `<DATA_DIR>/sqlite.db`, applies migrations,
and serves until interrupted. It does not daemonize.

See the canonical
[host API configuration](/reference/host-requirements-and-configuration/#host-api-configuration)
for flag sources and defaults.

The API can start without a reachable daemon, but base, template, environment,
SSH, and tunnel operations that need VM runtime services fail. The health API
checks only the API process, not the daemon.

This command is unavailable on macOS.

## `bastion start daemon` command

Starts the privileged Linux Cloud Hypervisor daemon on a Unix socket.

```sh
sudo bastion [--data-dir DATA_DIR] start daemon \
  [--socket SOCKET_PATH] [--socket-uid UID] [--socket-gid GID] \
  [--vm-uid UID] [--vm-gid GID] \
  [--log-format FORMAT] [--log-level LEVEL]
```

The command must run with effective UID `0`. It waits for the host API to create
`DATA_DIR`, seeds built-in actions, creates the socket, and serves until
interrupted. The socket UID and GID also apply to per-VM proxy sockets and the
base SSH key, so they must permit access by the host API user.

See the canonical
[daemon configuration](/reference/host-requirements-and-configuration/#daemon-configuration)
for flag sources and defaults.

This command is unavailable on macOS.

## `bastion system` commands

Checks, installs, and removes Linux host runtime assets. These commands operate
locally and do not call the API.

```sh
bastion [--data-dir DATA_DIR] system check
bastion [--data-dir DATA_DIR] system init [--with-utilities]
bastion [--data-dir DATA_DIR] system clean
```

| Command                        | Output and behavior                                                                                                                                                          |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `system check`                 | Checks reported OS and architecture, KVM and vhost-vsock device conditions, utility names on `PATH`, and required asset files. It does not boot a guest or test its network. |
| `system init`                  | Downloads and prepares Cloud Hypervisor and OpenCode assets. Prompts before installing missing supported utilities.                                                          |
| `system init --with-utilities` | Installs missing supported utility packages without prompting.                                                                                                               |
| `system clean`                 | Removes Bastion-managed Cloud Hypervisor and OpenCode assets. It leaves installed OS packages, bases, templates, environments, and SQLite metadata intact.                   |

`system init` does not build the base. See
[what the system check verifies](/reference/host-requirements-and-configuration/#what-the-system-check-verifies)
and the separate
[Linux VM host requirements](/reference/host-requirements-and-configuration/#linux-vm-host-requirements).

These commands are unavailable on macOS.

## `bastion base` commands

Manage the singleton base used by every template on the selected host API.

> **Warning:** Replacing a base can make every template created from the old
> content address unusable. Base archives also contain the guest SSH private
> key. Forced replacement is not atomic and can leave no installed base after
> an installation failure. Review
> [archive validation and trust](/reference/api/host/#archive-validation-and-trust)
> before replacing or distributing a base.

```sh
bastion base build [--force]
bastion base get
bastion base import --file ARCHIVE_PATH [--force]
bastion base export > ARCHIVE_PATH
```

| Command       | Flags                                 | Prerequisites                                               | Output                                                                      |
| ------------- | ------------------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------- |
| `base build`  | `--force` replaces an existing base   | Initialized system assets, running host API, running daemon | Progress to stderr; base metadata JSON to stdout.                           |
| `base get`    | None                                  | Existing base                                               | Base metadata JSON. Returns `404 Not Found` when absent.                    |
| `base import` | Required `--file`; optional `--force` | Running API and daemon; valid base archive                  | Upload progress and operation logs to stderr; base metadata JSON to stdout. |
| `base export` | None                                  | Existing base                                               | zstd-compressed tar archive to stdout; progress to stderr.                  |

Base metadata has this shape:

```json
{
  "contentAddress": "sha256:0123456789abcdef...",
  "createdAt": "2026-07-16T12:00:00Z",
  "updatedAt": "2026-07-16T12:00:00Z"
}
```

Build and import are NDJSON operations. Without `--force`, an existing base
produces a terminal `409 Conflict` error. There is no base remove command. A
failed export can leave a partial redirected file; check command success and
verify an out-of-band digest or signature before using it.

## `bastion secrets` commands

Manage secret values referenced by templates.

```sh
bastion secrets create [--key SECRET_KEY] --value SECRET_VALUE
bastion secrets list [--limit LIMIT] [--cursor CURSOR]
bastion secrets get (--id SECRET_ID | --key SECRET_KEY)
bastion secrets remove (--id SECRET_ID | --key SECRET_KEY)
```

| Command          | Output                                |
| ---------------- | ------------------------------------- |
| `secrets create` | Metadata only; never echoes `value`.  |
| `secrets list`   | Paginated metadata only.              |
| `secrets get`    | Full secret record including `value`. |
| `secrets remove` | Removed metadata without `value`.     |

`SECRET_ID`, `SECRET_KEY`, and `SECRET_VALUE` represent a generated ID,
optional user key, and nonempty value. A secret key cannot be blank or begin
with reserved prefix `sec_`. Keys are unique when present.

```json
{
  "id": "sec_16fd2706-8baf-433b-82eb-8c7fada847da",
  "key": "OPENAI_API_KEY",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

List flags default to `--limit 20`. The API treats nonpositive or unparsable
limits as `20` and clamps values above `100`. Pass the returned `cursor` to the
next request.

Removing a secret does not scrub values already persisted by init actions or
agent configuration in template and environment disks.

## `bastion templates` commands

Manage immutable prepared templates.

> **Warning:** Template archives can contain secret values resolved during init.
> Treat them as sensitive backup material, import only from a trusted source,
> and verify an out-of-band digest or signature. See
> [archive validation and trust](/reference/api/host/#archive-validation-and-trust).

```sh
bastion templates create [--key TEMPLATE_KEY] \
  (--config TEMPLATE_JSON | --file FILE_PATH)
bastion templates list [--limit LIMIT] [--cursor CURSOR]
bastion templates get (--id TEMPLATE_ID | --key TEMPLATE_KEY)
bastion templates export (--id TEMPLATE_ID | --key TEMPLATE_KEY) > ARCHIVE_PATH
bastion templates import [--key TEMPLATE_KEY] --file ARCHIVE_PATH
bastion templates remove (--id TEMPLATE_ID | --key TEMPLATE_KEY)
```

| Command            | Prerequisites and behavior                                                    | Output                                                     |
| ------------------ | ----------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `templates create` | Exactly one of `--config` or `--file`; current base; valid template schema    | Init logs to stderr; template metadata JSON to stdout.     |
| `templates list`   | Optional pagination                                                           | Metadata page without full `config`.                       |
| `templates get`    | Exactly one ID or key                                                         | Full template record including `config`.                   |
| `templates export` | Existing prepared template                                                    | Binary archive to stdout; progress to stderr.              |
| `templates import` | Matching current base and valid archive                                       | New template metadata JSON.                                |
| `templates remove` | No dependent environment records; database deletion precedes artifact cleanup | Removed full template record only when both steps succeed. |

`TEMPLATE_JSON` is inline JSON. `FILE_PATH` is a JSON configuration file for
create. An explicitly provided key must be nonblank and unique.

Template metadata:

```json
{
  "id": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "key": "dev",
  "baseContentAddress": "sha256:0123456789abcdef...",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

`templates get` adds the `config` object. Unkeyed records omit `key`.

Imports always create a new ID and do not retain the archived key. `--key`
assigns a new key. The archive contains a manifest and immutable qcow2 overlay,
not VM memory or cloud-init media. See the
[template configuration reference](/reference/template-configuration/). A
failed export can leave a partial redirected file even when it contains some
archive bytes.

Host template deletion is not atomic. If daemon artifact cleanup fails after
SQLite deletion, the command returns an error, the template is no longer
listable, and files can remain. See
[delete a template](/reference/api/host/#delete-a-template).

## `bastion env` commands

Create, inspect, and remove environments.

```sh
bastion env create \
  (--template-id TEMPLATE_ID | --template-key TEMPLATE_KEY) \
  [--key ENVIRONMENT_KEY] [--tag TAG]...
bastion env list [--limit LIMIT] [--cursor CURSOR] [--tag TAG]...
bastion env get (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY)
bastion env tunnels (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY)
bastion env remove (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY)
```

`--tag` has the short alias `-t` on `env create` and `env list`. The flag is
repeatable. Tags must be nonblank. Repeated list filters use AND semantics and
exact string matching.

| Command       | Behavior                                                                                   | Output                                            |
| ------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| `env create`  | Cold-boots from one template selector, starts managed services, then runs `actions.start`. | Logs to stderr; final environment JSON to stdout. |
| `env list`    | Reconciles returned records with the daemon and applies pagination and tag filters.        | Page of environment records.                      |
| `env get`     | Reconciles one record with the daemon.                                                     | One environment record.                           |
| `env tunnels` | Requires `running`; adds API URLs to template-registered tunnel entries.                   | `entries` array with `name`, `port`, and `url`.   |
| `env remove`  | Tears down runtime resources and deletes the record.                                       | A final record with status `removed`.             |

Creation, failure, and removal persistence differ between a host and cluster.
See
[host and cluster persistence](/reference/environment-states-and-streams/#host-and-cluster-persistence).

Example tunnel output:

```json
{
  "entries": [
    {
      "name": "frontend",
      "port": 3000,
      "url": "http://localhost:3148/v1/environments/env_550e8400-e29b-41d4-a716-446655440000/tunnels/frontend"
    }
  ]
}
```

The URL uses the resolved `API_URL`. It does not test whether a guest service is
listening. See [environment statuses](/reference/environment-states-and-streams/#environment-statuses).

## `bastion utilization` command

Returns host capacity and allocations.

```sh
bastion utilization
```

Output:

```json
{
  "vcpu": { "total": 16, "used": 2, "available": 14 },
  "memory": {
    "total": 34359738368,
    "used": 2147483648,
    "available": 32212254720
  },
  "volume": {
    "total": 1099511627776,
    "used": 21474836480,
    "available": 1078036791296
  }
}
```

Memory and volume values are bytes. Volume capacity is the total size of the
file system containing `DATA_DIR`, not a quota. Used values are declared
template allocations for environments in capacity-consuming states, not live
guest consumption. The host API resolves omitted template resources in its own
process, which can differ from daemon VM sizing. See
[VM sizing configuration](/reference/host-requirements-and-configuration/#vm-sizing-configuration).

## `bastion ssh` command

Opens an API-managed SSH session.

```sh
bastion ssh (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY)
bastion ssh (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY) -- COMMAND [ARG...]
```

With no command and terminal stdin/stdout, the CLI requests a PTY, enters local
raw mode, forwards terminal resize events, and opens a shell. Otherwise it
forwards stdin without a PTY. Remote stdout and stderr remain separate.

The CLI resolves `--key` with an environment get request because the API SSH
route accepts only an ID. A remote nonzero exit makes the command fail; the
current `bastion` process exits with status `1` rather than preserving the remote
status. SSH requires stored connection metadata and a `running` or `paused` host
environment.

Command arguments are not an argv-preserving transport. The API joins them into
one guest-shell string, so spaces and shell syntax can change their meaning. Do
not interpolate untrusted input. See
[SSH command and host-key security](/reference/environment-states-and-streams/#ssh-upgrade-stream).

## `bastion opencode` command

Runs a locally installed OpenCode TUI attached to the selected environment's
proxied server.

```sh
bastion opencode (--id ENVIRONMENT_ID | --key ENVIRONMENT_KEY)
```

The command requires `opencode` on local `PATH` and executes:

```text
opencode attach <ENVIRONMENT_AGENT_URL>
```

`ENVIRONMENT_AGENT_URL` is the resolved API URL ending in
`/v1/environments/.../agents/opencode`. The environment must be running and its
template must contain `agents.opencode`.

## `bastion proxy` command

Starts a long-running local reverse proxy for one registered environment tunnel.

```sh
bastion proxy \
  (--env-id ENVIRONMENT_ID | --env-key ENVIRONMENT_KEY) \
  --name TUNNEL_NAME [--host LOCAL_HOST] [--port LOCAL_PORT]
```

| Flag                        | Default               | Description                                                    |
| --------------------------- | --------------------- | -------------------------------------------------------------- |
| `--env-id ENVIRONMENT_ID`   | None                  | Environment ID; mutually exclusive with `--env-key`.           |
| `--env-key ENVIRONMENT_KEY` | None                  | Environment key; mutually exclusive with `--env-id`.           |
| `--name TUNNEL_NAME`        | Required              | Registered template tunnel name.                               |
| `--host LOCAL_HOST`         | `localhost`           | Local listen host.                                             |
| `--port LOCAL_PORT`         | Registered guest port | Local port from `0` through `65535`; `0` requests a free port. |

The command first verifies the tunnel through the API. When `--port` is omitted,
it tries the guest tunnel port locally; if that port is unavailable because of
address-in-use or permission errors, it falls back to a free port. An explicitly
selected unavailable port returns an error.

The proxy logs its local URL, target URL, and each request to stderr. It forwards
methods, bodies, paths, queries, streaming responses, and connection upgrades
such as WebSocket. `--host 0.0.0.0` exposes the local proxy on all interfaces and
requires the same network access care as exposing the API.

## `bastion mux` command

Creates or attaches to the tmux session named `bastion`.

```sh
bastion mux
```

The command requires `tmux` and an interactive terminal, unless it runs inside
an existing tmux client. It loads Bastion's tmux configuration, lists
environments through the selected API, and presents environment and connection
mode menus. Connection modes are SSH and OpenCode.

tmux windows use an environment key when present or its ID otherwise. Repeated
windows for the same environment receive suffixes such as `dev (2)`. The session
stores the resolved `BASTION_API_URL`, `BASTION_NAMESPACE_ID`, and
`BASTION_NAMESPACE_KEY`, so it also works with cluster namespaces.

The additional `bastion mux` subcommands shown by source code are hidden
implementation commands used inside the managed tmux session and are not a
user-facing interface.

## `bastion client` commands

Manage local persisted client options in `<DATA_DIR>/client.json`.

```sh
bastion [--data-dir DATA_DIR] client set api-url API_URL
bastion [--data-dir DATA_DIR] client set namespace-id NAMESPACE_ID
bastion [--data-dir DATA_DIR] client set namespace-key NAMESPACE_KEY
bastion [--data-dir DATA_DIR] client remove api-url
bastion [--data-dir DATA_DIR] client remove namespace-id
bastion [--data-dir DATA_DIR] client remove namespace-key
bastion [--data-dir DATA_DIR] client config
```

Set and remove commands produce no output on success. Setting one namespace
selector clears the other persisted selector. Removing the final persisted
value removes `client.json`. Creating the file requests mode `0600`, but an
update does not replace the mode of an existing file. Restrict broader existing
permissions separately.

`client config` prints resolved values and their source:

```json
{
  "dataDir": "/home/example/.bastion",
  "apiUrl": {
    "value": "http://localhost:3148",
    "source": "default"
  },
  "namespaceId": {
    "value": "",
    "source": "default"
  },
  "namespaceKey": {
    "value": "",
    "source": "default"
  }
}
```

Possible `source` values are `flag`, `environment`, `config`, and `default`.
Persisted API URLs follow the
[client URL rule](#global-client-flags). Namespace values must be nonblank.

## `bastion version` command

Prints one version string to stdout.

```sh
bastion version
```

Output from a development build:

```text
dev
```

Release builds print their injected release version.
