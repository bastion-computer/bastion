---
title: Cluster CLI
description: Start and configure the cluster control plane, manage nodes and namespaces, and run namespaced resource commands.
---

The cluster control plane presents one API in front of Linux x86_64 Bastion
hosts. `bastion start cluster` runs on Linux x86_64 and macOS arm64. Registered
nodes that host VMs must run the Linux host API and daemon.

For complete shared resource command syntax, see the
[host CLI reference](/reference/cli/host/). This page documents cluster-specific
defaults, requirements, orchestration, and limitations.

## Cluster command conventions

Command synopses use the following common uppercase placeholders:

| Placeholder                       | Value                                                  |
| --------------------------------- | ------------------------------------------------------ |
| `CLUSTER_API_URL`                 | Absolute cluster API URL.                              |
| `NODE_API_URL`                    | Absolute host API URL reachable by the control plane.  |
| `NODE_ID` or `NODE_KEY`           | Generated node ID or optional node key.                |
| `NAMESPACE_ID` or `NAMESPACE_KEY` | Generated namespace ID or optional namespace key.      |
| `CURSOR`                          | Pagination value returned by a previous list response. |

JSON results go to stdout. Progress and diagnostics go to stderr. Base and
template exports write binary archives to stdout. A failed export can leave a
partial redirected file; verify completion and the archive before use.

The standard Cobra help and completion commands documented under
[host command conventions](/reference/cli/host/#command-conventions) apply to
cluster commands too.

> **Important:** Bastion provides no native API authentication or TLS. Protect
> the cluster API and every registered node API with trusted networking and an
> external boundary that provides authentication and TLS. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## Cluster API URL selection

`bastion cluster nodes ...` and `bastion cluster namespaces ...` resolve their
API URL in this order:

1. `--api-url CLUSTER_API_URL`.
2. `BASTION_API_URL`.
3. Persisted `apiUrl` in `<DATA_DIR>/client.json`.
4. `http://localhost:3150`.

The `http://localhost:3150` default applies only under the top-level
`bastion cluster` command. Shared commands such as `bastion base`,
`bastion utilization`, `bastion secrets`, `bastion templates`, and
`bastion env` retain the host default `http://localhost:3148`. Set or persist
the cluster URL before using shared commands against the control plane.

```sh
bastion --api-url CLUSTER_API_URL cluster nodes list
bastion --api-url CLUSTER_API_URL --namespace-key NAMESPACE_KEY env list
```

The command blocks above are independent examples. `CLUSTER_API_URL` and
`NAMESPACE_KEY` must be replaced with actual values.

## `bastion start cluster` command

Starts the cluster HTTP API and applies Postgres migrations before listening.

```sh
bastion start cluster \
  [--addr ADDRESS] \
  [--database-url DATABASE_URL] \
  [--s3-bucket BUCKET] \
  [--s3-endpoint ENDPOINT_URL] \
  [--s3-region REGION] \
  [--s3-access-key-id ACCESS_KEY_ID] \
  [--s3-secret-access-key SECRET_ACCESS_KEY] \
  [--s3-use-path-style] \
  [--log-format FORMAT] [--log-level LEVEL]
```

The command requires a reachable Postgres database. It configures S3-compatible
archive storage only when `BUCKET` is nonempty. Without archive storage, node
and namespace metadata, health API, utilization API, and secret operations
remain available. Base build, import, and export; template create, import,
export, and delete; and environment creation return `424 Failed Dependency` when
no archive store is configured.

`FORMAT` is `json` or `text`. `LEVEL` is `debug`, `info`, `warn`, or `error`.
The server speaks plain HTTP and runs in the foreground.

See the canonical
[cluster control plane configuration](/reference/host-requirements-and-configuration/#cluster-control-plane-configuration)
for environment fallbacks, defaults, static AWS credential behavior, and the
session-token limitation.

> **Warning:** Cluster startup can expose database credentials in logs.
>
> At `info` and `debug` levels, it logs the full resolved database URL in the
> `database_url` field. Restrict log access and use `--log-level warn` or
> `--log-level error` pending a core redaction fix. Values passed through `--database-url`,
> `--s3-access-key-id`, and `--s3-secret-access-key` can also be exposed through
> process listings and shell history; prefer protected environment or AWS SDK
> chain sources where practical.

## `bastion cluster nodes` commands

Manage host API nodes available to the control plane.

```sh
bastion [--api-url CLUSTER_API_URL] cluster nodes create \
  [--key NODE_KEY] --url NODE_API_URL
bastion [--api-url CLUSTER_API_URL] cluster nodes list \
  [--limit LIMIT] [--cursor CURSOR]
bastion [--api-url CLUSTER_API_URL] cluster nodes get \
  (--id NODE_ID | --key NODE_KEY)
```

> **Warning:** Node removal does not migrate environments, stop VMs, or clean
> node-local derivative templates and secrets. Active source environment
> references can also prevent the database deletion. Review
> [cluster deletion and operation limits](/explanation/security-and-operational-limits/)
> before removing a node.

```sh
bastion [--api-url CLUSTER_API_URL] cluster nodes remove \
  (--id NODE_ID | --key NODE_KEY)
```

| Command        | Behavior                                                                                         | Output                                            |
| -------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| `nodes create` | Validates the URL, synchronizes the current cluster base when one exists, then records the node. | Streamed progress to stderr; node JSON to stdout. |
| `nodes list`   | Lists nodes in creation order with cursor pagination.                                            | Page of node records.                             |
| `nodes get`    | Gets one node by exactly one selector.                                                           | Node record.                                      |
| `nodes remove` | Deletes the control-plane node record only.                                                      | Removed node record when successful.              |

Use an absolute `http` or `https` `NODE_API_URL` with a host and an optional base
path. It must be reachable from the control plane. Do not include user
information, a query, or a fragment because the control plane appends `/v1/...`
routes by string concatenation. Current validation does not reject every
unsupported component. An `https` URL requires TLS outside the native node API.

Node keys are optional and unique. They cannot be blank or begin with reserved
prefix `node_`. Node IDs are generated as `node_` plus a UUID v4.

Use slash-free node keys. A key that contains `/` can be stored but cannot be
reliably addressed through the by-key CLI/API path; use the generated node ID
for such an existing record.

Node create does not perform a standalone health check. If no cluster base
exists, it can register an unreachable node after emitting a `no cluster base to
sync` progress message. If a base exists, creation loads its archive and imports
it into the node before recording the node.

Node output:

```json
{
  "id": "node_6ba7b810-9dad-41d1-80b4-00c04fd430c8",
  "key": "node-a",
  "url": "http://node-a.internal:3148",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

Unkeyed nodes omit `key`. List defaults to `--limit 20`; the API normalizes
nonpositive values to `20` and clamps values above `100`.

## `bastion cluster namespaces` commands

Manage namespaces for source resources.

```sh
bastion [--api-url CLUSTER_API_URL] cluster namespaces create \
  [--key NAMESPACE_KEY]
bastion [--api-url CLUSTER_API_URL] cluster namespaces list \
  [--limit LIMIT] [--cursor CURSOR]
bastion [--api-url CLUSTER_API_URL] cluster namespaces get \
  (--id NAMESPACE_ID | --key NAMESPACE_KEY)
```

> **Warning:** Namespace removal cascades source secret, template, environment,
> tag, and derivative-mapping records in Postgres. It does not first stop
> derivative VMs, remove node-local resources, or delete S3 objects. Review
> [cluster deletion and operation limits](/explanation/security-and-operational-limits/)
> before deleting a namespace.

```sh
bastion [--api-url CLUSTER_API_URL] cluster namespaces remove \
  (--id NAMESPACE_ID | --key NAMESPACE_KEY)
```

| Command             | Output                                               |
| ------------------- | ---------------------------------------------------- |
| `namespaces create` | New namespace record.                                |
| `namespaces list`   | Paginated namespace records in creation order.       |
| `namespaces get`    | One namespace record.                                |
| `namespaces remove` | Removed namespace record after the database cascade. |

Namespace keys are optional and unique. They cannot be blank or begin with
reserved prefix `ns_`. Namespace IDs are generated as `ns_` plus a UUID v4.

Use slash-free namespace keys. If an existing namespace key contains `/`, select
the namespace by its generated ID because the by-key route cannot reliably
address the key.

Namespace output:

```json
{
  "id": "ns_6f1ed002-ab9d-4f78-9b05-2f1f7cce3210",
  "key": "team-a",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

## Namespace selection for resource commands

Cluster secret, template, environment, SSH, tunnel, proxy, OpenCode, and mux
operations require exactly one namespace selector.

| Root flag                       | Environment             | Persisted setting                                |
| ------------------------------- | ----------------------- | ------------------------------------------------ |
| `--namespace-id NAMESPACE_ID`   | `BASTION_NAMESPACE_ID`  | `bastion client set namespace-id NAMESPACE_ID`   |
| `--namespace-key NAMESPACE_KEY` | `BASTION_NAMESPACE_KEY` | `bastion client set namespace-key NAMESPACE_KEY` |

Selection precedence is flag, environment, then persisted config. A selector
changes the API path to one of:

```text
/v1/namespaces/{NAMESPACE_ID}/secrets
/v1/namespaces/{NAMESPACE_ID}/templates
/v1/namespaces/{NAMESPACE_ID}/environments
```

or:

```text
/v1/namespaces/by-key/{NAMESPACE_KEY}/secrets
/v1/namespaces/by-key/{NAMESPACE_KEY}/templates
/v1/namespaces/by-key/{NAMESPACE_KEY}/environments
```

The braces mark placeholders and are not part of the request path. Namespace
selection does not apply to `base`, `utilization`, `cluster nodes`, or
`cluster namespaces` commands.

The cluster API also registers unprefixed secret, template, and environment
routes, but those requests return `400 Bad Request` with `namespace is required`.

The same slash limitation applies to source resource by-key routes. Use
slash-free secret, template, and environment keys, or select an existing
resource by its generated ID.

## Shared resource commands against a cluster

After selecting `CLUSTER_API_URL` and a namespace, these host CLI commands use
cluster source resources:

| Command family          | Cluster behavior                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------ |
| `bastion secrets ...`   | Stores source secrets in the selected namespace. Keys are unique within that namespace.                |
| `bastion templates ...` | Stores source templates and archives in the selected namespace. Keys are unique within that namespace. |
| `bastion env ...`       | Schedules source environments and node-local derivatives. Keys and lists are namespace-scoped.         |
| `bastion ssh ...`       | Resolves a source environment and proxies the framed SSH stream through its node.                      |
| `bastion proxy ...`     | Proxies a source environment tunnel through the cluster API and owning node.                           |
| `bastion opencode ...`  | Attaches through the cluster and node proxy chain.                                                     |
| `bastion mux`           | Lists and connects to environments in the selected namespace.                                          |

Command flags, aliases, stdout, and stderr behavior match the
[host CLI command reference](/reference/cli/host/#bastion-secrets-commands).

### Cluster base commands

The base is global to the cluster and ignores namespace selection.

```sh
bastion --api-url CLUSTER_API_URL base build [--force]
bastion --api-url CLUSTER_API_URL base get
bastion --api-url CLUSTER_API_URL base import --file ARCHIVE_PATH [--force]
bastion --api-url CLUSTER_API_URL base export > ARCHIVE_PATH
```

Cluster base build requires archive storage and at least one registered node.
Import requires archive storage but can run before nodes are registered. A later
`nodes create` synchronizes the stored base before recording the node. See
[cluster base routes](/reference/api/cluster/#cluster-base-routes) for the
canonical fan-out and persistence order.

> **Warning:** `--force` replacement is a multi-node operation without a public
> transaction or rollback command. Failure can leave node bases partially
> updated, and a successful replacement can invalidate templates in every
> namespace. Review
> [cluster operation limits](/explanation/security-and-operational-limits/).

Archive validation does not prove origin. Import only from a trusted source and
follow the
[archive validation and trust requirements](/reference/api/host/#archive-validation-and-trust).

### Cluster template commands

Template create requires a cluster base, archive storage, and at least one
registered node. Import requires archive storage and a matching cluster base but
does not require a current node. Template removal returns `424 Failed
Dependency` while a source environment uses the template. See
[namespaced template routes](/reference/api/cluster/#namespaced-template-routes)
for the canonical derivative, archive, and persistence order.

> **Warning:** Cluster template deletion is not atomic across Postgres and
> S3-compatible storage. If object deletion succeeds and the database commit
> fails, the source row can remain without its archive. An error therefore does
> not prove that both stores were rolled back.

Custom action packages are not distributed by the control plane. See
[cluster action availability](/reference/action-manifest/#cluster-availability).

### Cluster environment commands

Environment creation schedules from process-local resource resolution. Set all
three template resource fields explicitly; if no node fits vCPU, memory, and
volume, creation returns `424 Failed Dependency`. See
[namespaced environment routes](/reference/api/cluster/#namespaced-environment-routes)
for canonical scheduling and derivative behavior, and
[host and cluster persistence](/reference/environment-states-and-streams/#host-and-cluster-persistence)
for state visibility.

> **Warning:** Cluster environment deletion is not atomic. It removes the node
> environment before the source row; a database failure can leave a source that
> points to a missing derivative. It deletes the source row before optional
> derivative-template and secret cleanup; cleanup can then return an error even
> though the source is already gone.

## Cluster utilization command

```sh
bastion --api-url CLUSTER_API_URL utilization
```

The command sums each registered node's `vcpu`, `memory`, and `volume` totals,
used values, and available values. With no nodes, every value is zero. If any
node utilization request fails, the cluster request returns `424 Failed
Dependency` rather than a partial aggregate.

## Persisted cluster client profile

The local client config commands are the same for host and cluster use:

```sh
bastion client set api-url CLUSTER_API_URL
bastion client set namespace-id NAMESPACE_ID
bastion client set namespace-key NAMESPACE_KEY
bastion client config
```

Setting a namespace ID clears a persisted key, and setting a key clears a
persisted ID. See
[`bastion client` commands](/reference/cli/host/#bastion-client-commands) for
remove commands, precedence, and output shape.
